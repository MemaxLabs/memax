"use client";

import { useCallback, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  AccountForgottenView,
  AccountView,
  PasskeyView,
  SignInProvider,
} from "@/lib/v2/data/account";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import { IntentKeys } from "@/lib/v2/intent-keys";
import {
  createPasskey,
  PasskeyError,
  type PasskeyProblem,
} from "@/lib/v2/passkeys/webauthn";
import { useSource } from "./data";
import { settingsKeys } from "./settings";

/**
 * TanStack Query over `source.account` (lib/v2/data/account.ts): the
 * person, their sign-in methods and passkeys, and where they're signed
 * in; and the commands Settings › Account sends. One idempotency key per
 * intent, reused while a retry is the same command. A command that asks
 * for the person's passkey is answered by the re-check on the way
 * (lib/v2/passkeys/check.ts) and shows here only if they decline.
 */

export const accountKeys = {
  account: (kind: string) => ["v2", kind, "account"] as const,
  sessions: (kind: string) => ["v2", kind, "account", "sessions"] as const,
};

export function useAccount() {
  const source = useSource();
  return useQuery({
    queryKey: accountKeys.account(source.kind),
    queryFn: ({ signal }) => source.account.account({ signal }),
    initialData: source.account.peekAccount?.(),
    staleTime: 30_000,
  });
}

export function useSessions() {
  const source = useSource();
  return useQuery({
    queryKey: accountKeys.sessions(source.kind),
    queryFn: ({ signal }) => source.account.sessions({ signal }),
    initialData: source.account.peekSessions?.(),
    staleTime: 30_000,
  });
}

/** What a command came to. */
export type AccountOutcome<T = void> =
  | { ok: true; value: T }
  | { ok: false; failure: ReturnType<typeof toFailure> };

/** What adding a passkey came to: the browser's problem, or the server's. */
export type AddPasskeyOutcome =
  | { ok: true; value: PasskeyView }
  | { ok: false; problem: PasskeyProblem }
  | { ok: false; failure: ReturnType<typeof toFailure> };

export function useAccountCommands() {
  const source = useSource();
  const queryClient = useQueryClient();
  const keys = useRef(new IntentKeys()).current;
  const [pending, setPending] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: accountKeys.account(source.kind),
      }),
      // Settings › Security says whether the passkey check is on.
      queryClient.invalidateQueries({
        queryKey: settingsKeys.security(source.kind),
      }),
    ]);
  }, [queryClient, source.kind]);

  const run = useCallback(
    async <T,>(
      intent: string,
      fn: (key: string) => Promise<T>,
      after?: (value: T) => unknown,
    ): Promise<AccountOutcome<T>> => {
      setPending(intent);
      try {
        const value = await fn(keys.keyFor(intent));
        keys.settle(intent);
        await after?.(value);
        return { ok: true, value };
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        return { ok: false, failure };
      } finally {
        setPending(null);
      }
    },
    [keys],
  );

  const setAccount = useCallback(
    (value: AccountView) =>
      queryClient.setQueryData(accountKeys.account(source.kind), value),
    [queryClient, source.kind],
  );

  return {
    pending,
    rename: (name: string) =>
      run(
        `rename:${name}`,
        (idempotencyKey) => source.account.rename({ name, idempotencyKey }),
        setAccount,
      ),
    signOut: (id: string) =>
      run(
        `sign-out:${id}`,
        (idempotencyKey) => source.account.signOut({ id, idempotencyKey }),
        () =>
          queryClient.invalidateQueries({
            queryKey: accountKeys.sessions(source.kind),
          }),
      ),
    signOutOthers: () =>
      run(
        "sign-out-others",
        (idempotencyKey) => source.account.signOutOthers({ idempotencyKey }),
        () =>
          queryClient.invalidateQueries({
            queryKey: accountKeys.sessions(source.kind),
          }),
      ),
    connect: (method: SignInProvider, redirectUri: string) =>
      run(`connect:${method}`, (idempotencyKey) =>
        source.account.connectSignIn({ method, redirectUri, idempotencyKey }),
      ),
    disconnect: (method: SignInProvider) =>
      run(
        `disconnect:${method}`,
        (idempotencyKey) =>
          source.account.disconnectSignIn({ method, idempotencyKey }),
        setAccount,
      ),
    renamePasskey: (id: string, name: string) =>
      run(
        `rename-passkey:${id}:${name}`,
        (idempotencyKey) =>
          source.account.renamePasskey({ id, name, idempotencyKey }),
        refresh,
      ),
    removePasskey: (id: string) =>
      run(
        `remove-passkey:${id}`,
        (idempotencyKey) =>
          source.account.removePasskey({ id, idempotencyKey }),
        refresh,
      ),
    forget: (confirm: string) =>
      run<AccountForgottenView>(`forget:${confirm}`, (idempotencyKey) =>
        source.account.forget({ confirm, idempotencyKey }),
      ),
    /**
     * Adds a passkey: the server's options (asking for an existing passkey
     * first, when the sign-in isn't fresh), the browser's ceremony, then
     * the server's check of what it made. Each attempt is its own intent:
     * a challenge works once.
     */
    addPasskey: async (name?: string): Promise<AddPasskeyOutcome> => {
      setPending("add-passkey");
      try {
        const options = await source.account.startPasskey({
          idempotencyKey: crypto.randomUUID(),
        });
        const credential = await createPasskey(options);
        const value = await source.account.addPasskey({
          credential,
          name,
          idempotencyKey: crypto.randomUUID(),
        });
        await refresh();
        return { ok: true, value };
      } catch (err) {
        if (err instanceof PasskeyError) {
          return { ok: false, problem: err.problem };
        }
        return { ok: false, failure: toFailure(err) };
      } finally {
        setPending(null);
      }
    },
  };
}
