import { CommandFailedError } from "./command-error";
import type {
  AccountSource,
  AccountView,
  PasskeyView,
  SessionView,
} from "./account";
import { DEMO_NOW, DEMO_VIEWER } from "./demo-dataset";

/**
 * The demo's account (Account.png): Ziyang signs in with GitHub and a
 * passkey added Sep 2, Google isn't connected, and three sessions: this
 * browser, the CLI on ziyang-mbp two minutes ago, and a phone yesterday.
 * A session copy that commands change. Adding a passkey runs the
 * browser's real ceremony on options for this origin (a virtual
 * authenticator in Playwright), and keeps what it makes.
 */

const minutesAgo = (n: number) =>
  new Date(new Date(DEMO_NOW).getTime() - n * 60_000).toISOString();

export const DEMO_PASSKEY: PasskeyView = {
  id: "0192a7c0-0000-7000-8000-0000000ab001",
  name: "iCloud Keychain",
  provider: "iCloud Keychain",
  createdAt: "2026-09-02T10:12:00-07:00",
  lastUsedAt: minutesAgo(26 * 60),
  backupEligible: true,
  synced: true,
};

export const DEMO_ACCOUNT: AccountView = {
  id: DEMO_VIEWER.id ?? "0192a7c0-0000-7000-8000-0000000000a1",
  name: DEMO_VIEWER.name,
  email: "ziyang@example.com",
  initials: DEMO_VIEWER.initials,
  signIn: [
    {
      method: "github",
      connected: true,
      account: "ziyang",
      connectedAt: "2026-06-11T09:00:00-07:00",
    },
    { method: "google", connected: false, account: null, connectedAt: null },
    {
      method: "email",
      connected: true,
      account: "ziyang@example.com",
      connectedAt: null,
    },
  ],
  passkeys: [DEMO_PASSKEY],
  passkeyCheck: true,
  session: { signedInAt: minutesAgo(3 * 60), freshUntil: null },
};

export const DEMO_SESSIONS: SessionView[] = [
  {
    id: "0192a7c0-0000-7000-8000-0000000005e1",
    surface: "web",
    client: "Chrome on macOS",
    agent: null,
    city: "Vancouver",
    signedInAt: minutesAgo(3 * 60),
    lastUsedAt: DEMO_NOW,
    current: true,
  },
  {
    id: "0192a7c0-0000-7000-8000-0000000005e2",
    surface: "device",
    client: "memax CLI 2.0.0 on ziyang-mbp (macOS)",
    agent: null,
    city: "Vancouver",
    signedInAt: minutesAgo(5 * 24 * 60),
    lastUsedAt: minutesAgo(2),
    current: false,
  },
  {
    id: "0192a7c0-0000-7000-8000-0000000005e3",
    surface: "web",
    client: "Safari on iOS",
    agent: null,
    city: "Vancouver",
    signedInAt: minutesAgo(9 * 24 * 60),
    lastUsedAt: minutesAgo(22 * 60),
    current: false,
  },
];

export function createDemoAccount({
  commandDelayMs = 0,
}: { commandDelayMs?: number } = {}): AccountSource {
  const wait = () =>
    commandDelayMs > 0
      ? new Promise((r) => setTimeout(r, commandDelayMs))
      : Promise.resolve();
  let account: AccountView = DEMO_ACCOUNT;
  let sessions: SessionView[] = DEMO_SESSIONS;
  let next = 2;
  const withPasskeys = (passkeys: PasskeyView[]) => {
    account = { ...account, passkeys, passkeyCheck: passkeys.length > 0 };
  };
  const find = (id: string) => {
    const found = account.passkeys.find((p) => p.id === id);
    if (!found) throw new CommandFailedError({ kind: "not-found" });
    return found;
  };
  return {
    peekAccount: () => account,
    peekSessions: () => sessions,
    async account() {
      return account;
    },
    async rename({ name }) {
      await wait();
      account = { ...account, name };
      return account;
    },
    async sessions() {
      return sessions;
    },
    async signOut({ id }) {
      await wait();
      sessions = sessions.filter((s) => s.id !== id || s.current);
    },
    async signOutOthers() {
      await wait();
      const n = sessions.filter((s) => !s.current).length;
      sessions = sessions.filter((s) => s.current);
      return n;
    },
    async connectSignIn({ method, redirectUri }) {
      await wait();
      // No provider in the demo: come straight back, linked.
      account = {
        ...account,
        signIn: account.signIn.map((m) =>
          m.method === method
            ? {
                ...m,
                connected: true,
                account: account.email,
                connectedAt: DEMO_NOW,
              }
            : m,
        ),
      };
      const back = new URL(redirectUri, "http://demo.invalid");
      back.searchParams.set("account_linked", method);
      return `${back.pathname}${back.search}`;
    },
    async disconnectSignIn({ method }) {
      await wait();
      const providers = account.signIn.filter(
        (m) => m.method !== "email" && m.connected,
      );
      if (providers.length <= 1) {
        throw new CommandFailedError({ kind: "decided" });
      }
      account = {
        ...account,
        signIn: account.signIn.map((m) =>
          m.method === method
            ? { ...m, connected: false, account: null, connectedAt: null }
            : m,
        ),
      };
      return account;
    },
    async startPasskey() {
      await wait();
      const bytes = (n: number) =>
        btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(n))))
          .replace(/\+/g, "-")
          .replace(/\//g, "_")
          .replace(/=+$/, "");
      return {
        rp: {
          id: typeof window === "undefined" ? "localhost" : location.hostname,
          name: "Memax",
        },
        user: {
          id: bytes(16),
          name: account.email,
          displayName: account.name,
        },
        challenge: bytes(32),
        pubKeyCredParams: [
          { type: "public-key", alg: -7 },
          { type: "public-key", alg: -257 },
        ],
        timeout: 300_000,
        excludeCredentials: [],
        authenticatorSelection: {
          residentKey: "required",
          requireResidentKey: true,
          userVerification: "required",
        },
        attestation: "none",
      };
    },
    async addPasskey({ name }) {
      await wait();
      const pk: PasskeyView = {
        id: `0192a7c0-0000-7000-8000-0000000ab${String(next++).padStart(3, "0")}`,
        name: name?.trim() || "Passkey",
        provider: null,
        createdAt: DEMO_NOW,
        lastUsedAt: null,
        backupEligible: true,
        synced: true,
      };
      withPasskeys([...account.passkeys, pk]);
      return pk;
    },
    async renamePasskey({ id, name }) {
      await wait();
      const renamed = { ...find(id), name };
      withPasskeys(account.passkeys.map((p) => (p.id === id ? renamed : p)));
      return renamed;
    },
    async removePasskey({ id }) {
      await wait();
      const gone = find(id);
      withPasskeys(account.passkeys.filter((p) => p.id !== id));
      return gone;
    },
    async forget() {
      await wait();
      return {
        spaces: 2,
        agents: 5,
        teamSpacesKept: 1,
        sessions: sessions.length,
        passkeys: account.passkeys.length,
      };
    },
  };
}
