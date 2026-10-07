import type { CreationOptionsJSON, CredentialJSON } from "../passkeys/webauthn";

/**
 * Settings › Account (Account.png): the person, how they sign in, their
 * passkeys, where they're signed in, and forgetting the account. Part of
 * LedgerDataSource (source.ts) as `source.account`; account-sdk.ts
 * (memax.v2.account, memax.v2.sessions) and account-demo.ts (the board's
 * data) implement it.
 *
 * Commands take one idempotency key per user action. A decision that
 * asks for the person's passkey (removing one, disconnecting GitHub,
 * forgetting the account) is answered on the way by the SDK client's
 * re-check (lib/v2/passkeys/check.ts); it never shows here.
 */

export type SignInProvider = "github" | "google";
export type SignInMethodKind = SignInProvider | "email";

export interface SignInMethodView {
  method: SignInMethodKind;
  connected: boolean;
  /** The provider account it signs in as; the account's email for `email`. */
  account: string | null;
  connectedAt: string | null;
}

export interface PasskeyView {
  id: string;
  name: string;
  /** "iCloud Keychain", "1Password", …, when Memax knows the provider. */
  provider: string | null;
  createdAt: string;
  lastUsedAt: string | null;
  /** Its provider syncs it across the person's devices. */
  backupEligible: boolean;
  synced: boolean;
}

export interface AccountView {
  id: string;
  name: string;
  email: string;
  /** The person's stamp on receipts ("ZZ"). */
  initials: string;
  signIn: SignInMethodView[];
  passkeys: PasskeyView[];
  /** Decisions that need the person ask for their passkey. */
  passkeyCheck: boolean;
  /** This session's sign-in: until when it counts as fresh, to add a way in. */
  session: { signedInAt: string | null; freshUntil: string | null };
}

export type SessionSurface = "web" | "cli" | "device" | "mcp";

export interface SessionView {
  id: string;
  surface: SessionSurface;
  /** "Chrome on macOS", "memax CLI 2.0.0 on ziyang-mbp (macOS)", "Claude". */
  client: string;
  /** An MCP client's agent. */
  agent: string | null;
  city: string | null;
  signedInAt: string;
  lastUsedAt: string;
  current: boolean;
}

export interface AccountForgottenView {
  spaces: number;
  agents: number;
  teamSpacesKept: number;
  sessions: number;
  passkeys: number;
}

export interface AccountSource {
  /** The demo answers at once, so screenshots never catch a loading frame. */
  peekAccount?(): AccountView | undefined;
  peekSessions?(): SessionView[] | undefined;
  account(input?: { signal?: AbortSignal }): Promise<AccountView>;
  rename(input: { name: string; idempotencyKey: string }): Promise<AccountView>;
  sessions(input?: { signal?: AbortSignal }): Promise<SessionView[]>;
  signOut(input: { id: string; idempotencyKey: string }): Promise<void>;
  /** Signs out every session but this one; answers how many ended. */
  signOutOthers(input: { idempotencyKey: string }): Promise<number>;
  /** The provider's sign-in page; it comes back to `redirectUri`. */
  connectSignIn(input: {
    method: SignInProvider;
    redirectUri: string;
    idempotencyKey: string;
  }): Promise<string>;
  disconnectSignIn(input: {
    method: SignInProvider;
    idempotencyKey: string;
  }): Promise<AccountView>;
  /** The options for navigator.credentials.create. */
  startPasskey(input: { idempotencyKey: string }): Promise<CreationOptionsJSON>;
  addPasskey(input: {
    credential: CredentialJSON;
    name?: string;
    idempotencyKey: string;
  }): Promise<PasskeyView>;
  renamePasskey(input: {
    id: string;
    name: string;
    idempotencyKey: string;
  }): Promise<PasskeyView>;
  removePasskey(input: {
    id: string;
    idempotencyKey: string;
  }): Promise<PasskeyView>;
  forget(input: {
    confirm: string;
    idempotencyKey: string;
  }): Promise<AccountForgottenView>;
}

/** Whether the typed confirmation is the account's email. */
export function confirmsEmail(typed: string, email: string): boolean {
  return (
    email.trim() !== "" &&
    typed.trim().toLowerCase() === email.trim().toLowerCase()
  );
}

/** Whether this session can add a way in without a passkey now. */
export function isFresh(account: AccountView, now: Date): boolean {
  const until = account.session.freshUntil;
  return until !== null && new Date(until).getTime() > now.getTime();
}
