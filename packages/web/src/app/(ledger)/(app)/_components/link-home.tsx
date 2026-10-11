"use client";

import { Icon } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { cliCommand } from "@/lib/v2/cli";
import { useToast } from "./toasts";
import styles from "./link-home.module.css";

/** The command that writes a Personal space into a machine's agent files, for this build's CLI and API. */
export function linkHomeCommand(): string {
  return cliCommand("link --home");
}

/**
 * What to run on a computer so Personal is written there (personal-layer
 * design §6.1, §8): a sentence, then `memax link --home` as this build's
 * CLI spells it, with a copy button. The command wraps rather than
 * scrolls, so a long staging prefix stays readable in a side panel.
 */
export function LinkHome({ lead }: { lead: string }) {
  const { t } = useLocale();
  const toast = useToast();
  const command = linkHomeCommand();
  const label = t.ledger.machines.panel.copy;
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      toast({ text: t.ledger.app.toast.copiedCommand });
    } catch {
      toast({ state: "proposed", text: t.ledger.brief.toast.copyFailed });
    }
  };
  return (
    <div className={styles.box}>
      <p className={styles.lead}>{lead}</p>
      <div className={styles.command}>
        <code>{command}</code>
        <button
          type="button"
          className={styles.copy}
          aria-label={label}
          title={label}
          onClick={() => void copy()}
        >
          <Icon name="copy" size={14} />
        </button>
      </div>
    </div>
  );
}
