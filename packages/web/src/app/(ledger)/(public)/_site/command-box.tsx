"use client";

import { useEffect, useRef, useState } from "react";
import { Icon } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import styles from "./landing.module.css";

/**
 * The Landing's one command on ink, with a copy button (Landing.png). The
 * command is this build's (lib/v2/cli.ts: staging names its own API and
 * CLI tag). Copying says so in a polite live region; a browser that
 * refuses the clipboard is told to select the command instead.
 */
export function CommandBox({ command }: { command: string }) {
  const { t } = useLocale();
  const copy = t.ledger.public.landing;
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const onCopy = async () => {
    clearTimeout(timer.current);
    try {
      await navigator.clipboard.writeText(command);
      setState("copied");
    } catch {
      setState("failed");
    }
    timer.current = setTimeout(() => setState("idle"), 2400);
  };

  return (
    <div className={styles.command} role="group" aria-label={copy.command}>
      <code className={styles.commandText}>
        <span className={styles.prompt} aria-hidden="true">
          ›
        </span>{" "}
        {command}
      </code>
      <button
        type="button"
        className={styles.copy}
        onClick={onCopy}
        aria-label={copy.copy}
        title={copy.copy}
      >
        <Icon name={state === "copied" ? "check" : "copy"} size={14} />
      </button>
      <span className="mx-sr" role="status">
        {state === "copied"
          ? copy.copied
          : state === "failed"
            ? copy.copyFailed
            : ""}
      </span>
    </div>
  );
}
