"use client";

import { useState } from "react";
import { useLocale } from "@/i18n";
import { isFresh, type AccountView } from "@/lib/v2/data/account";
import { passkeyReason } from "@/lib/v2/records-copy";
import { useAccountCommands } from "../../_lib/account";
import { useSource } from "../../_lib/data";
import { useToast } from "../../_components/toasts";

/** Where adding a passkey stands, for the button and the line under it. */
export type AddState =
  | { kind: "idle" }
  /** Adding a way in needs a sign-in from the last few minutes. */
  | { kind: "sign-in" }
  | { kind: "problem"; text: string };

/**
 * Adding a passkey from Account: a person with none needs a fresh sign-in
 * (the server says so too, `needs_sign_in`); one with a passkey confirms
 * with it when the sign-in isn't fresh (the re-check, answered on the
 * way). Then the browser makes the passkey and the server checks it.
 */
export function useAddPasskey(account: AccountView | undefined) {
  const { t } = useLocale();
  const copy = t.ledger.account;
  const source = useSource();
  const commands = useAccountCommands();
  const toast = useToast();
  const [state, setState] = useState<AddState>({ kind: "idle" });

  const add = async () => {
    if (!account) return;
    if (account.passkeys.length === 0 && !isFresh(account, source.now())) {
      setState({ kind: "sign-in" });
      return;
    }
    setState({ kind: "idle" });
    const out = await commands.addPasskey();
    if (out.ok) {
      toast({ state: "kept", text: copy.passkeys.addedToast });
      return;
    }
    if ("problem" in out) {
      const p = copy.passkeys;
      setState({
        kind: "problem",
        text:
          out.problem === "unsupported"
            ? p.unsupported
            : out.problem === "exists"
              ? p.exists
              : out.problem === "cancelled"
                ? p.cancelled
                : p.failed,
      });
      return;
    }
    const f = out.failure;
    if (f.kind === "refused" && f.code === "needs_sign_in") {
      setState({ kind: "sign-in" });
      return;
    }
    setState({
      kind: "problem",
      text:
        f.kind === "passkey"
          ? passkeyReason(t.ledger.records.failure, f.failure)
          : f.kind === "unreachable"
            ? t.ledger.records.failure.unreachable
            : copy.passkeys.failed,
    });
  };

  return {
    add,
    state,
    pending: commands.pending === "add-passkey",
    reset: () => setState({ kind: "idle" }),
  };
}

/** Where "Sign in again" goes: a fresh web sign-in that comes back here. */
export const SIGN_IN_AGAIN =
  "/signin?again=1&next=" + encodeURIComponent("/settings/account");
