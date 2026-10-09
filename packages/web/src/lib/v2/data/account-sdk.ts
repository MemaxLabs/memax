import type { V2 } from "memax-sdk";
import type { CreationOptionsJSON } from "../passkeys/webauthn";
import type {
  AccountForgottenView,
  AccountSource,
  AccountView,
  PasskeyView,
  SessionView,
} from "./account";
import type { V2Client } from "./sdk-records";

/**
 * Settings › Account through memax.v2.account (`/v2/me/account`, its
 * passkeys and sign-in methods) and memax.v2.sessions.
 */

export function passkeyOf(p: V2.Passkey): PasskeyView {
  return {
    id: p.id,
    name: p.name,
    provider: p.provider ?? null,
    createdAt: p.created_at,
    lastUsedAt: p.last_used_at ?? null,
    backupEligible: p.backup_eligible,
    synced: p.synced,
  };
}

export function accountOf(a: V2.Account): AccountView {
  return {
    id: a.id,
    name: a.name,
    email: a.email,
    initials: a.initials,
    signIn: a.sign_in_methods.map((m) => ({
      method: m.method,
      connected: m.connected,
      account: m.account ?? null,
      connectedAt: m.connected_at ?? null,
    })),
    passkeys: a.passkeys.map(passkeyOf),
    passkeyCheck: a.passkey_check,
    session: {
      signedInAt: a.session.signed_in_at ?? null,
      freshUntil: a.session.fresh_until ?? null,
    },
  };
}

export function sessionOf(s: V2.Session): SessionView {
  return {
    id: s.id,
    surface: s.surface,
    client: s.client,
    agent: s.agent ?? null,
    city: s.city ?? null,
    signedInAt: s.signed_in_at,
    lastUsedAt: s.last_used_at,
    current: s.current,
  };
}

function forgottenOf(f: V2.AccountForgotten): AccountForgottenView {
  return {
    spaces: f.spaces,
    agents: f.agents,
    teamSpacesKept: f.team_spaces_kept,
    sessions: f.sessions,
    passkeys: f.passkeys,
  };
}

export function createSdkAccount(client: V2Client): AccountSource {
  const account = client.v2.account;
  return {
    async account({ signal } = {}) {
      return accountOf(await account.get({ signal }));
    },
    async rename({ name, idempotencyKey }) {
      return accountOf(await account.update({ name }, { idempotencyKey }));
    },
    async sessions({ signal } = {}) {
      const list = await client.v2.sessions.list({ signal });
      return list.items.map(sessionOf);
    },
    async signOut({ id, idempotencyKey }) {
      await client.v2.sessions.revoke(id, { idempotencyKey });
    },
    async signOutOthers({ idempotencyKey }) {
      return (await client.v2.sessions.revokeOthers({ idempotencyKey }))
        .revoked;
    },
    async connectSignIn({ method, redirectUri, idempotencyKey }) {
      return (
        await account.connectSignIn(method, redirectUri, { idempotencyKey })
      ).url;
    },
    async disconnectSignIn({ method, idempotencyKey }) {
      return accountOf(
        await account.disconnectSignIn(method, { idempotencyKey }),
      );
    },
    async startPasskey({ idempotencyKey }) {
      const reg = await account.passkeys.startRegistration({ idempotencyKey });
      return reg.options as CreationOptionsJSON;
    },
    async addPasskey({ credential, name, idempotencyKey }) {
      return passkeyOf(
        await account.passkeys.add(credential, name, { idempotencyKey }),
      );
    },
    async renamePasskey({ id, name, idempotencyKey }) {
      return passkeyOf(
        await account.passkeys.rename(id, name, { idempotencyKey }),
      );
    },
    async removePasskey({ id, idempotencyKey }) {
      return passkeyOf(await account.passkeys.remove(id, { idempotencyKey }));
    },
    async forget({ confirm, idempotencyKey }) {
      return forgottenOf(await account.forget(confirm, { idempotencyKey }));
    },
  };
}
