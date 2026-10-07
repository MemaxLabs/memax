"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Icon, StateMark, type IconName } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import type { AccountView, SignInProvider } from "@/lib/v2/data/account";
import { formatShortDate } from "@/lib/v2/copy";
import { passkeyReason } from "@/lib/v2/records-copy";
import { useAccountCommands } from "../../_lib/account";
import { useToast } from "../../_components/toasts";
import { usePlace } from "../../_places/place";
import { SIGN_IN_AGAIN, useAddPasskey } from "./add-passkey";
import { PasskeysDialog } from "./passkeys-dialog";
import styles from "./account.module.css";

const PROVIDERS: { method: SignInProvider; icon: IconName }[] = [
  { method: "github", icon: "commit" },
  { method: "google", icon: "globe" },
];

/**
 * Sign in with (Account.png): GitHub and Google, connected or not, and
 * passkeys. Connecting goes through the provider's page and comes back
 * here; it needs a fresh sign-in (or the passkey). Disconnecting asks for
 * the passkey when the person has one. Email codes always work: the
 * profile's email says where they go.
 */
export function SignInPanel({
  account,
  addRequested,
}: {
  account: AccountView;
  /** ?add=passkey: the nudge after a decision sent the person here. */
  addRequested: boolean;
}) {
  const { t, locale } = useLocale();
  const copy = t.ledger.account.signIn;
  const commands = useAccountCommands();
  const adding = useAddPasskey(account);
  const toast = useToast();
  const { timeZone } = usePlace();
  const [manage, setManage] = useState(false);
  const [alert, setAlert] = useState<string | null>(null);
  const [needsSignIn, setNeedsSignIn] = useState(false);
  const addRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (addRequested) addRef.current?.focus();
  }, [addRequested]);

  const name = (m: SignInProvider) => copy[m];

  const connect = async (method: SignInProvider) => {
    setAlert(null);
    setNeedsSignIn(false);
    const back = new URL("/settings/account", window.location.origin);
    const out = await commands.connect(method, back.toString());
    if (out.ok) {
      window.location.assign(out.value);
      return;
    }
    const f = out.failure;
    if (f.kind === "refused" && f.code === "needs_sign_in") {
      setNeedsSignIn(true);
      return;
    }
    setAlert(
      f.kind === "passkey"
        ? passkeyReason(t.ledger.records.failure, f.failure)
        : copy.failed,
    );
  };

  const disconnect = async (method: SignInProvider) => {
    setAlert(null);
    const out = await commands.disconnect(method);
    if (out.ok) {
      toast({
        text: interpolate(copy.disconnected, { provider: name(method) }),
      });
      return;
    }
    const f = out.failure;
    setAlert(
      f.kind === "decided"
        ? copy.lastWay
        : f.kind === "passkey"
          ? passkeyReason(t.ledger.records.failure, f.failure)
          : copy.failed,
    );
  };

  const first = account.passkeys[0];
  const passkeyMeta =
    account.passkeys.length > 1
      ? interpolate(copy.passkeys, { n: account.passkeys.length })
      : first
        ? interpolate(copy.added, {
            date: formatShortDate(new Date(first.createdAt), timeZone, locale),
          })
        : null;
  const signInAgain = needsSignIn || adding.state.kind === "sign-in";
  const problem = adding.state.kind === "problem" ? adding.state.text : alert;

  return (
    <section className="mx-panel" aria-labelledby="sign-in-title" id="passkeys">
      <header className="mx-panel-head">
        <h2 id="sign-in-title" className="mx-panel-title">
          {copy.title}
        </h2>
      </header>
      {PROVIDERS.map(({ method, icon }) => {
        const m = account.signIn.find((s) => s.method === method);
        const connected = Boolean(m?.connected);
        return (
          <div className={styles.row} key={method}>
            <Icon name={icon} />
            <span className={styles.name}>
              {name(method)}
              {connected && m?.account ? (
                <span className="mx-meta"> · {m.account}</span>
              ) : null}
            </span>
            {connected ? (
              <StateMark state="kept" label={copy.connected} />
            ) : (
              <span className={styles.quiet}>{copy.notConnected}</span>
            )}
            {connected ? (
              <Button
                variant="quiet"
                size="sm"
                pending={commands.pending === `disconnect:${method}`}
                onClick={() => void disconnect(method)}
              >
                {copy.disconnect}
              </Button>
            ) : (
              <Button
                variant="secondary"
                size="sm"
                pending={commands.pending === `connect:${method}`}
                onClick={() => void connect(method)}
              >
                {copy.connect}
              </Button>
            )}
          </div>
        );
      })}
      <div className={styles.row}>
        <Icon name="shield" />
        <span className={styles.name}>
          {copy.passkey}
          {passkeyMeta ? (
            <span className="mx-meta"> · {passkeyMeta}</span>
          ) : null}
        </span>
        {account.passkeyCheck ? (
          <StateMark state="kept" label={copy.on} />
        ) : (
          <span className={styles.quiet} title={copy.offHint}>
            {copy.off}
          </span>
        )}
        {account.passkeys.length > 0 ? (
          <Button
            variant="quiet"
            size="sm"
            ref={addRef}
            onClick={() => setManage(true)}
          >
            {copy.manage}
          </Button>
        ) : (
          <Button
            variant="secondary"
            size="sm"
            ref={addRef}
            pending={adding.pending}
            onClick={() => void adding.add()}
          >
            {copy.add}
          </Button>
        )}
      </div>
      {signInAgain ? (
        <div className={styles.alert} role="alert">
          <span>{copy.needsSignIn}</span>
          <Button variant="secondary" size="sm" href={SIGN_IN_AGAIN}>
            {copy.signInAgain}
          </Button>
        </div>
      ) : problem ? (
        <div className={styles.alert} role="alert">
          <span>{problem}</span>
        </div>
      ) : account.passkeys.length === 0 ? (
        <div className={styles.alert}>
          <span>{copy.offHint}</span>
        </div>
      ) : null}
      <PasskeysDialog
        open={manage}
        onOpenChange={setManage}
        account={account}
      />
    </section>
  );
}
