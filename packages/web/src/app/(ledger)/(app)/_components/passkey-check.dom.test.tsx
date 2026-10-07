// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Memax, type PasskeyCheck } from "memax-sdk";
import {
  PASSKEY_SUGGESTED_EVENT,
  answerPasskeyCheck,
} from "@/lib/v2/passkeys/check";
import { renderPlace } from "../_places/test-frame";
import { PasskeyCheckHost } from "./passkey-check";

// The passkey re-check in the frame: the SDK client's needs_passkey goes
// to a small layer; "Use passkey" asks the browser (user verification
// required) and the same request goes again with the answer; "Not now"
// answers nothing. And the nudge after a decision made without a passkey.

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  usePathname: () => "/memax-v2/review",
  useSearchParams: () => new URLSearchParams(),
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, loading: false }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  push.mockReset();
});

const OPTIONS = {
  challenge: "Y2hhbGxlbmdlLWZvci10aGUta2VlcA",
  timeout: 300000,
  rpId: "memax.app",
  allowCredentials: [{ type: "public-key" as const, id: "Y3JlZA" }],
  userVerification: "required" as const,
};

const ANSWER = {
  id: "Y3JlZA",
  rawId: "Y3JlZA",
  type: "public-key",
  response: { clientDataJSON: "e30", authenticatorData: "AA", signature: "AA" },
  clientExtensionResults: {},
};

/** A browser whose get() answers (or throws) as given. */
function withWebAuthn(get: () => Promise<unknown>) {
  vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
  const spy = vi.fn(get);
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get: spy },
  });
  return spy;
}

const check = (): PasskeyCheck => ({
  options: OPTIONS,
  expiresAt: new Date(Date.now() + 300_000).toISOString(),
  message: "You have a passkey, so keeping M-0219 asks for it.",
});

describe("the passkey re-check", () => {
  it("asks, and answers with the browser's assertion", async () => {
    const get = withWebAuthn(async () => ({ toJSON: () => ANSWER }));
    renderPlace(<PasskeyCheckHost />);
    let answer: Promise<unknown> = Promise.resolve();
    act(() => {
      answer = answerPasskeyCheck(check());
    });
    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    expect(dialog.textContent).toContain(
      "You have a passkey, so this decision asks for it. Only you can make it.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Use passkey" }));
    await expect(answer).resolves.toEqual(ANSWER);
    const publicKey = (
      get.mock.calls[0] as unknown as [
        { publicKey: PublicKeyCredentialRequestOptions },
      ]
    )[0].publicKey;
    expect(publicKey.userVerification).toBe("required");
    expect(publicKey.rpId).toBe("memax.app");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("stays open when the prompt was closed, and Not now answers nothing", async () => {
    const closed = Object.assign(new Error("closed"), {
      name: "NotAllowedError",
    });
    withWebAuthn(async () => Promise.reject(closed));
    renderPlace(<PasskeyCheckHost />);
    let answer: Promise<unknown> = Promise.resolve();
    act(() => {
      answer = answerPasskeyCheck(check());
    });
    await screen.findByRole("dialog", { name: "Confirm it's you" });
    fireEvent.click(screen.getByRole("button", { name: "Use passkey" }));
    await screen.findByText(
      "The passkey prompt closed before it finished. Try again, or choose Not now.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Not now" }));
    await expect(answer).resolves.toBeNull();
  });

  it("answers nothing with no frame to ask in", async () => {
    await expect(answerPasskeyCheck(check())).resolves.toBeNull();
  });

  it("sends the same request again with the answer: same key, body and If-Match", async () => {
    withWebAuthn(async () => ({ toJSON: () => ANSWER }));
    renderPlace(<PasskeyCheckHost />);
    const refused = {
      error: {
        code: "needs_passkey",
        message: "You have a passkey, so keeping M-0219 asks for it.",
        details: {
          policy: { effect: "refuse", code: "needs_passkey" },
          passkey: {
            options: OPTIONS,
            expires_at: new Date(Date.now() + 300_000).toISOString(),
          },
        },
      },
    };
    const kept = {
      data: {
        outcome: "applied",
        policy: { effect: "apply" },
        memory: null,
        receipts: [{ assurance: "human_web_verified" }],
      },
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify(refused), { status: 403 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(kept), { status: 200 }),
      );
    const memax = new Memax({
      apiUrl: "/api/proxy",
      fetch: fetchMock,
      maxRetries: 0,
      passkeyCheck: answerPasskeyCheck,
    });
    let result: Promise<unknown> = Promise.resolve();
    act(() => {
      result = memax.v2.memories.keep(
        "M-0219",
        {},
        {
          space: "memax-v2",
          idempotencyKey: "keep-M-0219",
          ifMatch: 2,
        },
      );
    });
    fireEvent.click(await screen.findByRole("button", { name: "Use passkey" }));
    await expect(result).resolves.toMatchObject({ outcome: "applied" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const [first, second] = fetchMock.mock.calls as [
      string,
      RequestInit & { headers: Record<string, string> },
    ][];
    expect(second[0]).toBe(first[0]);
    expect(second[1].body).toBe(first[1].body);
    expect(second[1].headers["Idempotency-Key"]).toBe("keep-M-0219");
    expect(second[1].headers["If-Match"]).toBe('"2"');
    expect(first[1].headers["X-Memax-Passkey"]).toBeUndefined();
    const sent = second[1].headers["X-Memax-Passkey"]!;
    expect(
      JSON.parse(atob(sent.replace(/-/g, "+").replace(/_/g, "/"))),
    ).toEqual(ANSWER);
  });

  it("suggests a passkey once after a decision made without one", async () => {
    renderPlace(<PasskeyCheckHost />);
    act(() => {
      window.dispatchEvent(new CustomEvent(PASSKEY_SUGGESTED_EVENT));
      window.dispatchEvent(new CustomEvent(PASSKEY_SUGGESTED_EVENT));
    });
    await screen.findByText(
      "Done. Add a passkey, and decisions like this one will ask for it.",
    );
    expect(screen.getAllByText(/^Done\. Add a passkey/)).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
    expect(push).toHaveBeenCalledWith("/settings/account?add=passkey");
  });
});
