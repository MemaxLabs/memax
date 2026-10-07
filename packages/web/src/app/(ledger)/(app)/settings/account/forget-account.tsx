"use client";

import { useRef, useState, type FormEvent } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button, Field } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { confirmsEmail, type AccountView } from "@/lib/v2/data/account";
import { KeyScopeBoundary } from "@/lib/v2/keymap/react";
import { passkeyReason } from "@/lib/v2/records-copy";
import { useAccountCommands } from "../../_lib/account";
import layer from "../../_components/layer.module.css";
import styles from "./account.module.css";

/**
 * The danger zone (Account.png, "Delete your account" in the board's
 * words; Forget in the product's): forgets everything in Personal and in
 * the projects the person owns, everywhere it was compiled, through the
 * ledger's ForgetAccount. The person types their email; with a passkey,
 * the re-check asks for it on the way. Then every session is signed out,
 * this one too, and the page goes to sign-in.
 */
export function ForgetAccountPanel({ account }: { account: AccountView }) {
  const { t } = useLocale();
  const copy = t.ledger.account.forget;
  const [open, setOpen] = useState(false);
  return (
    <section className={styles.danger} aria-labelledby="forget-title">
      <div className={styles.dangerText}>
        <h2 id="forget-title">{copy.title}</h2>
        <p>{copy.body}</p>
      </div>
      <Button variant="danger" onClick={() => setOpen(true)}>
        {copy.button}
      </Button>
      <Dialog.Root open={open} onOpenChange={(next) => setOpen(next)}>
        <Dialog.Portal>
          <Dialog.Backdrop className="mx-cmd-scrim" />
          {open ? (
            <ForgetBody account={account} onClose={() => setOpen(false)} />
          ) : null}
        </Dialog.Portal>
      </Dialog.Root>
    </section>
  );
}

function ForgetBody({
  account,
  onClose,
}: {
  account: AccountView;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.account.forget;
  const commands = useAccountCommands();
  const [typed, setTyped] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const fieldRef = useRef<HTMLInputElement | null>(null);
  const matches = confirmsEmail(typed, account.email);

  const forget = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!matches) {
      setProblem(copy.mismatch);
      return;
    }
    setProblem(null);
    const out = await commands.forget(typed.trim());
    if (out.ok) {
      // Every session is signed out on the server, this one too: clear its
      // cookies and start over at sign-in.
      try {
        await fetch("/api/auth/logout", { method: "POST" });
      } catch {
        // The cookies no longer work anyway.
      }
      window.location.assign("/signin");
      return;
    }
    const f = out.failure;
    setProblem(
      f.kind === "passkey"
        ? passkeyReason(t.ledger.records.failure, f.failure)
        : f.kind === "unreachable"
          ? t.ledger.records.failure.unreachable
          : copy.failed,
    );
  };

  return (
    <Dialog.Popup
      className={layer.layer}
      initialFocus={fieldRef}
      aria-labelledby="forget-account-title"
    >
      <KeyScopeBoundary name="forget-account" modal>
        <form className={layer.card} onSubmit={(e) => void forget(e)}>
          <header className={layer.head}>
            <Dialog.Title id="forget-account-title" className={layer.title}>
              {copy.dialogTitle}
            </Dialog.Title>
          </header>
          <div className={layer.body}>
            <p className="mx-section-label">{copy.what}</p>
            <ul className={layer.list}>
              <li>{copy.spaces}</li>
              <li>{copy.agents}</li>
              <li>{copy.team}</li>
              <li>{copy.sessions}</li>
            </ul>
            <Field
              ref={fieldRef}
              label={interpolate(copy.confirmLabel, { email: account.email })}
              value={typed}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setTyped(e.target.value)}
              error={problem ?? undefined}
            />
          </div>
          <footer className={layer.foot}>
            <Button variant="quiet" size="sm" onClick={onClose}>
              {copy.cancel}
            </Button>
            <Button
              type="submit"
              variant="danger"
              size="sm"
              pending={commands.pending !== null}
              disabled={!matches}
              disabledReason={copy.mismatch}
            >
              {commands.pending !== null ? copy.working : copy.confirm}
            </Button>
          </footer>
        </form>
      </KeyScopeBoundary>
    </Dialog.Popup>
  );
}
