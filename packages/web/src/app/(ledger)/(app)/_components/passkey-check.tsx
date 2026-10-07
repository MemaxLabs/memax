"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Dialog } from "@base-ui/react/dialog";
import { Button, Icon } from "@memaxlabs/ledger";
import type { PasskeyCheck } from "memax-sdk";
import { useLocale } from "@/i18n";
import { KeyScopeBoundary } from "@/lib/v2/keymap/react";
import {
  PASSKEY_SUGGESTED_EVENT,
  setPasskeyAsker,
} from "@/lib/v2/passkeys/check";
import {
  getPasskey,
  PasskeyError,
  type RequestOptionsJSON,
} from "@/lib/v2/passkeys/webauthn";
import { useToast } from "./toasts";
import styles from "./layer.module.css";

interface Asked {
  check: PasskeyCheck;
  answer: (credential: unknown | null) => void;
}

/**
 * The passkey re-check, for the whole frame (plan §5.15): when the API
 * asks a person with a passkey to confirm a decision (403
 * `needs_passkey`), the SDK client asks here. A small layer says why and
 * waits for "Use passkey", whose press is the user gesture the browser's
 * prompt needs (Safari refuses one that follows a network round trip);
 * then the browser asks for the passkey with user verification, and the
 * answer goes back to the SDK, which sends the same request again. "Not
 * now" (or Escape) answers nothing: the command fails as `needs_passkey`
 * and nothing changed.
 *
 * It also offers a passkey once per visit after a decision that needed a
 * person went through without one (`policy.suggest`).
 */
export function PasskeyCheckHost() {
  const { t } = useLocale();
  const copy = t.ledger.account;
  const toast = useToast();
  const router = useRouter();
  const [asked, setAsked] = useState<Asked | null>(null);
  // The last check asked, kept while the layer closes so it leaves with
  // its scrim (the portal unmounts both once the exit ends).
  const [shown, setShown] = useState<Asked | null>(null);
  const nudged = useRef(false);

  useEffect(
    () =>
      setPasskeyAsker(
        (check) =>
          new Promise((answer) => {
            const next = { check, answer };
            setAsked(next);
            setShown(next);
          }),
      ),
    [],
  );

  useEffect(() => {
    const onSuggest = () => {
      if (nudged.current) return;
      nudged.current = true;
      toast({
        text: copy.nudge.text,
        action: {
          label: copy.nudge.action,
          onClick: () => router.push("/settings/account?add=passkey"),
        },
      });
    };
    window.addEventListener(PASSKEY_SUGGESTED_EVENT, onSuggest);
    return () => window.removeEventListener(PASSKEY_SUGGESTED_EVENT, onSuggest);
  }, [copy.nudge, router, toast]);

  const finish = (credential: unknown | null) => {
    asked?.answer(credential);
    setAsked(null);
  };

  return (
    <Dialog.Root
      open={asked !== null}
      onOpenChange={(open) => {
        if (!open) finish(null);
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="mx-cmd-scrim" />
        {shown ? (
          <CheckBody
            key={shown.check.options.challenge}
            check={shown.check}
            onAnswer={finish}
          />
        ) : null}
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function CheckBody({
  check,
  onAnswer,
}: {
  check: PasskeyCheck;
  onAnswer: (credential: unknown | null) => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.account.check;
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const confirmRef = useRef<HTMLButtonElement | null>(null);

  const use = async () => {
    if (busy) return;
    if (check.expiresAt && Date.parse(check.expiresAt) <= Date.now()) {
      setProblem(copy.expired);
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const credential = await getPasskey(check.options as RequestOptionsJSON);
      onAnswer(credential);
    } catch (err) {
      const kind = err instanceof PasskeyError ? err.problem : "failed";
      setProblem(kind === "unsupported" ? copy.unsupported : copy.cancelled);
      setBusy(false);
    }
  };

  return (
    <Dialog.Popup
      className={styles.layer}
      initialFocus={confirmRef}
      aria-labelledby="passkey-check-title"
      aria-describedby="passkey-check-body"
    >
      <KeyScopeBoundary name="passkey-check" modal>
        <section className={styles.card}>
          <header className={styles.head}>
            <Dialog.Title id="passkey-check-title" className={styles.title}>
              {copy.title}
            </Dialog.Title>
            <Icon name="shield" />
          </header>
          <div className={styles.body} id="passkey-check-body">
            <p>{copy.body}</p>
            {problem ? (
              <p className={styles.problem} role="alert">
                {problem}
              </p>
            ) : null}
            <p className="mx-meta">{copy.lost}</p>
          </div>
          <footer className={styles.foot}>
            <Button variant="quiet" size="sm" onClick={() => onAnswer(null)}>
              {copy.cancel}
            </Button>
            <Button
              variant="primary"
              size="sm"
              icon="shield"
              ref={confirmRef}
              pending={busy}
              onClick={() => void use()}
            >
              {busy ? copy.waiting : copy.use}
            </Button>
          </footer>
        </section>
      </KeyScopeBoundary>
    </Dialog.Popup>
  );
}
