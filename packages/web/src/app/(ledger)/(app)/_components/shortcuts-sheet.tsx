"use client";

import { Fragment, useRef } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button, Kbd } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { keycaps } from "@/lib/v2/keymap/keys";
import {
  KeyScopeBoundary,
  useHotkey,
  usePlatform,
} from "@/lib/v2/keymap/react";
import {
  KEYMAP,
  type KeyBinding,
  type KeyGroup,
} from "@/lib/v2/keymap/registry";
import { useOverlays } from "../_lib/overlays";
import styles from "./shortcuts-sheet.module.css";

/**
 * The `?` sheet (Shortcuts.png), generated from the keymap registry so
 * it lists exactly the keys the app binds, on this platform. Forget is
 * in it, with no key, on purpose.
 */
export function ShortcutsSheet() {
  const { keysOpen, setKeysOpen } = useOverlays();
  const { t } = useLocale();
  const copy = t.ledger.app.keys;
  const sheet = useRef<HTMLDivElement>(null);
  return (
    <Dialog.Root open={keysOpen} onOpenChange={(open) => setKeysOpen(open)}>
      <Dialog.Portal>
        <Dialog.Backdrop className={styles.scrim} />
        {/* Focus lands on the sheet itself: it's read, not operated, and
            Tab still reaches Close. */}
        <Dialog.Popup ref={sheet} className={styles.sheet} initialFocus={sheet}>
          <KeyScopeBoundary name="keys" modal>
            <SheetKeys onClose={() => setKeysOpen(false)} />
            <header className={styles.head}>
              <Dialog.Title className={styles.title}>{copy.title}</Dialog.Title>
              <Dialog.Close
                render={<Button variant="quiet" size="sm" kbd="Esc" />}
              >
                {copy.close}
              </Dialog.Close>
            </header>
            <KeyColumns />
          </KeyScopeBoundary>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** `?` again closes the sheet; ⌘K swaps it for Ask. */
function SheetKeys({ onClose }: { onClose: () => void }) {
  const { openCommand } = useOverlays();
  useHotkey("help.keys", onClose);
  useHotkey("command.open", () => openCommand("ask"));
  return null;
}

const COLUMNS: KeyGroup[] = ["everywhere", "review", "memory"];

function KeyColumns() {
  const { t } = useLocale();
  const copy = t.ledger.app.keys;
  const byGroup = (group: KeyGroup) =>
    (KEYMAP as readonly KeyBinding[]).filter((b) => b.group === group);
  const pages = byGroup("pages");
  return (
    <>
      <div className={styles.columns}>
        {COLUMNS.map((group) => (
          <section key={group} aria-labelledby={`keys-${group}`}>
            <h3 id={`keys-${group}`} className={styles.group}>
              {copy.groups[group]}
            </h3>
            <dl className={styles.list}>
              {byGroup(group).map((binding) => (
                <KeyRow key={binding.id} binding={binding} />
              ))}
            </dl>
          </section>
        ))}
      </div>
      {pages.length > 0 ? (
        <section className={styles.pages} aria-labelledby="keys-pages">
          <h3 id="keys-pages" className={styles.group}>
            {copy.groups.pages}
          </h3>
          <dl className={styles.pageList}>
            {pages.map((binding) => (
              <KeyRow key={binding.id} binding={binding} />
            ))}
          </dl>
        </section>
      ) : null}
    </>
  );
}

function KeyRow({ binding }: { binding: KeyBinding }) {
  const { t } = useLocale();
  const copy = t.ledger.app.keys;
  const platform = usePlatform();
  const label =
    copy.actions[binding.id as keyof typeof copy.actions] ?? binding.id;
  const alternatives = (binding.keys ?? []).map((spec) =>
    keycaps(spec, platform),
  );
  const shown =
    binding.display === "range" && alternatives.length > 1
      ? [alternatives[0], alternatives[alternatives.length - 1]]
      : alternatives;
  return (
    <div className={styles.row} data-binding={binding.id}>
      <dt>{label}</dt>
      <dd className={styles.caps}>
        {binding.keys === null ? (
          <span className="mx-meta">{copy.none}</span>
        ) : (
          shown.map((caps, i) => (
            <Fragment key={i}>
              {i > 0 && binding.display === "range" ? (
                <span className="mx-meta">{copy.to}</span>
              ) : null}
              {caps.map((cap, j) => (
                <Kbd key={j}>{cap}</Kbd>
              ))}
            </Fragment>
          ))
        )}
      </dd>
    </div>
  );
}
