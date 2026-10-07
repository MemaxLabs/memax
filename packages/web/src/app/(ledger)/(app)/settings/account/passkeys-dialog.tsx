"use client";

import { useRef, useState, type FormEvent } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button, Field, Icon } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { passkeyMeta } from "@/lib/v2/account-copy";
import type { AccountView, PasskeyView } from "@/lib/v2/data/account";
import { passkeysSupported } from "@/lib/v2/passkeys/webauthn";
import { passkeyReason } from "@/lib/v2/records-copy";
import { KeyScopeBoundary } from "@/lib/v2/keymap/react";
import { useAccountCommands } from "../../_lib/account";
import { useToast } from "../../_components/toasts";
import { usePlace } from "../../_places/place";
import { SIGN_IN_AGAIN, useAddPasskey } from "./add-passkey";
import layer from "../../_components/layer.module.css";

/**
 * Manage passkeys (Account's "Manage"): each passkey with its provider,
 * when it was added and last used, and whether it syncs; rename one,
 * remove one (which asks for a passkey: the re-check), or add another.
 */
export function PasskeysDialog({
  open,
  onOpenChange,
  account,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  account: AccountView;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Backdrop className="mx-cmd-scrim" />
        {/* Always rendered: the portal mounts it for each opening (so each
            starts afresh) and keeps it while it closes, so the scrim's exit
            ends with the popup's. */}
        <PasskeysBody account={account} onClose={() => onOpenChange(false)} />
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function PasskeysBody({
  account,
  onClose,
}: {
  account: AccountView;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.account.passkeys;
  const adding = useAddPasskey(account);
  const doneRef = useRef<HTMLButtonElement | null>(null);
  const supported = passkeysSupported();

  return (
    <Dialog.Popup
      className={`${layer.layer} ${layer.wide}`}
      initialFocus={doneRef}
      aria-labelledby="passkeys-title"
    >
      <KeyScopeBoundary name="passkeys" modal>
        <section className={layer.card}>
          <header className={layer.head}>
            <Dialog.Title id="passkeys-title" className={layer.title}>
              {copy.title}
            </Dialog.Title>
            <Dialog.Close
              render={
                <Button
                  variant="quiet"
                  size="sm"
                  icon="x"
                  aria-label={copy.close}
                />
              }
            />
          </header>
          <div className={layer.body}>
            <p>{copy.lede}</p>
            {account.passkeys.length === 0 ? (
              <p className="mx-meta">{copy.none}</p>
            ) : (
              <ul className={layer.rows} aria-label={copy.title}>
                {account.passkeys.map((p) => (
                  <PasskeyRow
                    key={p.id}
                    passkey={p}
                    last={account.passkeys.length === 1}
                  />
                ))}
              </ul>
            )}
            {adding.state.kind === "sign-in" ? (
              <p role="alert">
                {t.ledger.account.signIn.needsSignIn}{" "}
                <a href={SIGN_IN_AGAIN}>
                  {t.ledger.account.signIn.signInAgain}
                </a>
              </p>
            ) : adding.state.kind === "problem" ? (
              <p className={layer.problem} role="alert">
                {adding.state.text}
              </p>
            ) : null}
            {!supported ? <p className="mx-meta">{copy.unsupported}</p> : null}
          </div>
          <footer className={layer.foot}>
            <Button
              variant="secondary"
              size="sm"
              icon="plus"
              pending={adding.pending}
              disabled={!supported}
              disabledReason={!supported ? copy.unsupported : undefined}
              onClick={() => void adding.add()}
            >
              {copy.add}
            </Button>
            <Button variant="primary" size="sm" ref={doneRef} onClick={onClose}>
              {copy.close}
            </Button>
          </footer>
        </section>
      </KeyScopeBoundary>
    </Dialog.Popup>
  );
}

function PasskeyRow({
  passkey,
  last,
}: {
  passkey: PasskeyView;
  last: boolean;
}) {
  const { t, locale } = useLocale();
  const copy = t.ledger.account.passkeys;
  const { timeZone } = usePlace();
  const commands = useAccountCommands();
  const toast = useToast();
  const [mode, setMode] = useState<"view" | "rename" | "remove">("view");
  const [name, setName] = useState(passkey.name);
  const [problem, setProblem] = useState<string | null>(null);

  const reason = (f: Parameters<typeof passkeyReason>[1] | undefined) =>
    f === undefined ? copy.failed : passkeyReason(t.ledger.records.failure, f);

  const rename = async (event?: FormEvent) => {
    event?.preventDefault();
    const next = name.trim().replace(/\s+/g, " ");
    if (!next || next === passkey.name) {
      setMode("view");
      return;
    }
    const out = await commands.renamePasskey(passkey.id, next);
    if (out.ok) {
      setMode("view");
      setProblem(null);
      toast({ text: interpolate(copy.renamed, { name: out.value.name }) });
    } else {
      setProblem(copy.failed);
    }
  };

  const remove = async () => {
    const out = await commands.removePasskey(passkey.id);
    if (out.ok) {
      toast({ text: interpolate(copy.removed, { name: passkey.name }) });
      return;
    }
    setProblem(
      out.failure.kind === "passkey"
        ? reason(out.failure.failure)
        : copy.failed,
    );
  };

  return (
    <li className={layer.row}>
      <Icon name="shield" />
      {mode === "rename" ? (
        <form className={layer.rowText} onSubmit={(e) => void rename(e)}>
          <Field
            aria-label={interpolate(copy.renameLabel, { name: passkey.name })}
            value={name}
            maxLength={64}
            autoFocus
            onChange={(e) => setName(e.target.value)}
          />
        </form>
      ) : (
        <span className={layer.rowText}>
          <span className={layer.rowName}>{passkey.name}</span>
          <span className={layer.rowMeta}>
            {passkeyMeta(t.ledger.account, passkey, timeZone, locale)}
          </span>
          {problem ? (
            <span className={`${layer.rowMeta} ${layer.problem}`} role="alert">
              {problem}
            </span>
          ) : null}
        </span>
      )}
      <span className={layer.actions}>
        {mode === "rename" ? (
          <Button
            variant="secondary"
            size="sm"
            pending={commands.pending !== null}
            onClick={() => void rename()}
          >
            {copy.renameDone}
          </Button>
        ) : (
          <>
            <Button
              variant="quiet"
              size="sm"
              onClick={() => {
                setName(passkey.name);
                setMode("rename");
              }}
            >
              {copy.rename}
            </Button>
            <Button variant="quiet" size="sm" onClick={() => setMode("remove")}>
              {copy.remove}
            </Button>
          </>
        )}
      </span>
      {mode === "remove" ? (
        <div className={layer.confirm}>
          <span>
            {interpolate(copy.removeConfirm, { name: passkey.name })}
            {last ? ` ${copy.removeLast}` : ""}
          </span>
          <span className={layer.actions}>
            <Button variant="quiet" size="sm" onClick={() => setMode("view")}>
              {copy.keep}
            </Button>
            <Button
              variant="danger"
              size="sm"
              pending={commands.pending !== null}
              onClick={() => void remove()}
            >
              {interpolate(copy.removeYes, { name: passkey.name })}
            </Button>
          </span>
        </div>
      ) : null}
    </li>
  );
}
