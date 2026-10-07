import { afterEach, describe, expect, it, vi } from "vitest";
import type { PasskeyCheck } from "memax-sdk";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  lastUsedText,
  linkErrorText,
  passkeyMeta,
  sessionIcon,
  sessionLabel,
} from "../account-copy";
import { accountOf, passkeyOf, sessionOf } from "../data/account-sdk";
import { confirmsEmail, isFresh } from "../data/account";
import {
  DEMO_ACCOUNT,
  DEMO_PASSKEY,
  DEMO_SESSIONS,
} from "../data/account-demo";
import { answerPasskeyCheck, setPasskeyAsker, suggestsPasskey } from "./check";
import {
  base64urlToBytes,
  bytesToBase64url,
  creationOptions,
  credentialJSON,
  requestOptions,
} from "./webauthn";

afterEach(() => vi.unstubAllGlobals());

const NOW = new Date("2026-10-05T14:40:00-07:00");
const TZ = "America/Vancouver";

describe("base64url", () => {
  it("round-trips bytes, without padding", () => {
    const bytes = new Uint8Array([0, 251, 255, 62, 63, 1, 2]);
    const text = bytesToBase64url(bytes);
    expect(text).not.toMatch(/[+/=]/);
    expect([...base64urlToBytes(text)]).toEqual([...bytes]);
  });
});

describe("options and credentials without the browser's own JSON", () => {
  it("decodes the options' binary members", () => {
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    const o = creationOptions({
      rp: { id: "memax.app", name: "Memax" },
      user: { id: "dXNlcg", name: "ziyang@example.com", displayName: "Ziyang" },
      challenge: "AQID",
      pubKeyCredParams: [{ type: "public-key", alg: -7 }],
      timeout: 300000,
      excludeCredentials: [
        { type: "public-key", id: "BAU", transports: ["internal"] },
      ],
      authenticatorSelection: {
        residentKey: "required",
        requireResidentKey: true,
        userVerification: "required",
      },
      attestation: "none",
    });
    expect([...(o.challenge as Uint8Array)]).toEqual([1, 2, 3]);
    expect(new TextDecoder().decode(o.user.id as Uint8Array)).toBe("user");
    expect([...(o.excludeCredentials![0]!.id as Uint8Array)]).toEqual([4, 5]);
    const r = requestOptions({
      challenge: "AQID",
      timeout: 1,
      rpId: "memax.app",
      allowCredentials: [],
      userVerification: "required",
    });
    expect(r.userVerification).toBe("required");
    expect(r.rpId).toBe("memax.app");
  });

  it("prefers the browser's parser", () => {
    const parse = vi.fn(() => ({ parsed: true }));
    vi.stubGlobal(
      "PublicKeyCredential",
      Object.assign(function PublicKeyCredential() {}, {
        parseRequestOptionsFromJSON: parse,
      }),
    );
    expect(
      requestOptions({
        challenge: "AQID",
        timeout: 1,
        rpId: "x",
        allowCredentials: [],
        userVerification: "required",
      }),
    ).toEqual({ parsed: true });
  });

  it("serialises an assertion by hand when toJSON is missing", () => {
    const buf = (n: number[]) => new Uint8Array(n).buffer;
    const json = credentialJSON({
      id: "AQ",
      rawId: buf([1]),
      type: "public-key",
      authenticatorAttachment: "platform",
      getClientExtensionResults: () => ({}),
      response: {
        clientDataJSON: buf([123, 125]),
        authenticatorData: buf([0]),
        signature: buf([9]),
        userHandle: buf([7]),
      },
    } as unknown as PublicKeyCredential);
    expect(json).toMatchObject({
      id: "AQ",
      rawId: "AQ",
      response: {
        clientDataJSON: "e30",
        authenticatorData: "AA",
        signature: "CQ",
        userHandle: "Bw",
      },
    });
  });
});

describe("the re-check bridge", () => {
  it("asks the page one at a time, and answers null with no page", async () => {
    const check = {
      options: { challenge: "c" },
      expiresAt: "",
      message: "",
    } as unknown as PasskeyCheck;
    await expect(answerPasskeyCheck(check)).resolves.toBeNull();
    const seen: string[] = [];
    const undo = setPasskeyAsker(async (c) => {
      seen.push((c as { options: { challenge: string } }).options.challenge);
      await new Promise((r) => setTimeout(r, 5));
      return { answer: seen.length };
    });
    const [a, b] = await Promise.all([
      answerPasskeyCheck({
        ...check,
        options: { ...check.options, challenge: "1" },
      }),
      answerPasskeyCheck({
        ...check,
        options: { ...check.options, challenge: "2" },
      }),
    ]);
    expect(seen).toEqual(["1", "2"]);
    expect([a, b]).toEqual([{ answer: 1 }, { answer: 2 }]);
    undo();
    await expect(answerPasskeyCheck(check)).resolves.toBeNull();
  });

  it("reads the suggestion from a command's answer", () => {
    expect(
      suggestsPasskey({
        data: { policy: { effect: "apply", suggest: "passkey" } },
      }),
    ).toBe(true);
    expect(suggestsPasskey({ data: { policy: { effect: "apply" } } })).toBe(
      false,
    );
    expect(suggestsPasskey(null)).toBe(false);
  });
});

describe("Account's words", () => {
  const copyEn = en.ledger.account;
  const copyZh = zh.ledger.account;

  it("names sessions as the board does", () => {
    const [web, cli, phone] = DEMO_SESSIONS as [
      (typeof DEMO_SESSIONS)[0],
      (typeof DEMO_SESSIONS)[0],
      (typeof DEMO_SESSIONS)[0],
    ];
    expect(sessionLabel(copyEn, web)).toEqual({
      label: "This browser",
      meta: null,
    });
    expect(sessionLabel(copyEn, cli)).toEqual({
      label: "memax CLI on ziyang-mbp",
      meta: "2.0.0",
    });
    expect(sessionLabel(copyZh, cli)).toEqual({
      label: "ziyang-mbp 上的 memax CLI",
      meta: "2.0.0",
    });
    expect(sessionLabel(copyEn, { ...cli, client: "memax CLI 0.9.0" })).toEqual(
      { label: "memax CLI", meta: "0.9.0" },
    );
    expect(
      sessionLabel(copyEn, {
        ...web,
        current: false,
        surface: "mcp",
        client: "Claude",
      }).label,
    ).toBe("Claude over MCP");
    expect(sessionLabel(copyEn, phone).label).toBe("Safari on iOS");
    expect([sessionIcon(web), sessionIcon(cli), sessionIcon(phone)]).toEqual([
      "globe",
      "terminal",
      "today",
    ]);
  });

  it("says when a session was last used", () => {
    const ago = (minutes: number) =>
      new Date(NOW.getTime() - minutes * 60000).toISOString();
    expect(lastUsedText(copyEn, ago(0), NOW, TZ, "en")).toBe("now");
    expect(lastUsedText(copyEn, ago(2), NOW, TZ, "en")).toBe("2 min ago");
    expect(lastUsedText(copyEn, ago(180), NOW, TZ, "en")).toBe("3 h ago");
    expect(lastUsedText(copyEn, ago(22 * 60), NOW, TZ, "en")).toBe("yesterday");
    expect(lastUsedText(copyEn, ago(4 * 24 * 60), NOW, TZ, "en")).toBe(
      "4 days ago",
    );
    expect(lastUsedText(copyZh, ago(2), NOW, TZ, "zh")).toBe("2 分钟前");
    expect(lastUsedText(copyEn, ago(30 * 24 * 60), NOW, TZ, "en")).toBe(
      "Sep 5",
    );
  });

  it("describes a passkey, and why a provider wasn't linked", () => {
    expect(passkeyMeta(copyEn, DEMO_PASSKEY, TZ, "en")).toBe(
      "Added Sep 2 · last used Oct 4 · Synced across your devices",
    );
    expect(
      passkeyMeta(
        copyEn,
        {
          ...DEMO_PASSKEY,
          name: "Laptop",
          lastUsedAt: null,
          synced: false,
          backupEligible: false,
        },
        TZ,
        "en",
      ),
    ).toBe("iCloud Keychain · Added Sep 2 · not used yet · On one device");
    expect(linkErrorText(copyEn, "email_conflict")).toBe(
      "its email belongs to another Memax account.",
    );
    expect(linkErrorText(copyEn, "nonsense")).toBe(
      "something went wrong. Try again.",
    );
  });
});

describe("the account's data", () => {
  it("maps the wire", () => {
    const a = accountOf({
      id: "u",
      name: "Ziyang Zeng",
      email: "ziyang@example.com",
      initials: "ZZ",
      sign_in_methods: [
        { method: "github", connected: true, account: "ziyang" },
        { method: "google", connected: false },
      ],
      passkeys: [
        {
          id: "p",
          name: "iCloud Keychain",
          created_at: "2026-09-02T17:12:00Z",
          backup_eligible: true,
          synced: true,
          transports: [],
        },
      ],
      passkey_check: true,
      session: {
        signed_in_at: "2026-10-05T18:00:00Z",
        fresh_until: "2026-10-05T18:10:00Z",
      },
    });
    expect(a.signIn[1]).toEqual({
      method: "google",
      connected: false,
      account: null,
      connectedAt: null,
    });
    expect(a.passkeys[0]).toEqual(
      passkeyOf({
        id: "p",
        name: "iCloud Keychain",
        created_at: "2026-09-02T17:12:00Z",
        backup_eligible: true,
        synced: true,
        transports: [],
      }),
    );
    expect(a.passkeys[0]!.provider).toBeNull();
    expect(isFresh(a, new Date("2026-10-05T18:05:00Z"))).toBe(true);
    expect(isFresh(a, new Date("2026-10-05T18:11:00Z"))).toBe(false);
    expect(isFresh(DEMO_ACCOUNT, NOW)).toBe(false);
    expect(
      sessionOf({
        id: "s",
        surface: "device",
        client: "memax CLI",
        signed_in_at: "x",
        last_used_at: "y",
        expires_at: "z",
        current: false,
      }).agent,
    ).toBeNull();
  });

  it("checks the typed email", () => {
    expect(confirmsEmail(" Ziyang@Example.com ", "ziyang@example.com")).toBe(
      true,
    );
    expect(confirmsEmail("ziyang@example.co", "ziyang@example.com")).toBe(
      false,
    );
    expect(confirmsEmail("", "")).toBe(false);
  });
});
