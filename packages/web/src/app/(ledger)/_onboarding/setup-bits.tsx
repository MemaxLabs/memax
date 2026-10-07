"use client";

import { useSyncExternalStore, type ReactNode } from "react";

const noSubscribe = () => () => {};

/**
 * This app's host ("memax.app"), for what the CLI prints. The server
 * renders the production host and the browser its own, without a
 * hydration mismatch.
 */
export function useAppHost(): string {
  return useSyncExternalStore(
    noSubscribe,
    () => window.location.host,
    () => "memax.app",
  );
}
import { Button, Icon, type ButtonProps } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { useToast } from "../(app)/_components/toasts";
import styles from "./setup-bits.module.css";

function useCopyCommand(command: string) {
  const { t } = useLocale();
  const copy = t.ledger.onboarding.frame;
  const toast = useToast();
  return async () => {
    try {
      await navigator.clipboard.writeText(command);
      toast({ text: copy.copied });
    } catch {
      toast({ state: "proposed", text: copy.copyFailed });
    }
  };
}

/** A button that copies a command, and says so. */
export function CopyCommandButton({
  command,
  children,
  ...props
}: Omit<ButtonProps, "onClick"> & { command: string }) {
  const copyCommand = useCopyCommand(command);
  return (
    <Button {...props} onClick={() => void copyCommand()}>
      {children}
    </Button>
  );
}

/** A command to run, on night, with a copy button (EmptySpace.png). */
export function CommandBox({ command }: { command: string }) {
  const { t } = useLocale();
  const copyCommand = useCopyCommand(command);
  const label = t.ledger.onboarding.frame.copy;
  return (
    <div className={styles.command}>
      <span className={styles.prompt} aria-hidden="true">
        ›
      </span>
      <code>{command}</code>
      <button
        type="button"
        className={styles.copy}
        aria-label={label}
        title={label}
        onClick={() => void copyCommand()}
      >
        <Icon name="copy" size={14} />
      </button>
    </div>
  );
}

export interface SetupStepItem {
  title: string;
  detail: ReactNode;
  state: "done" | "now" | "later";
  /** Under the detail, for the step in progress. */
  extra?: ReactNode;
}

/** FirstRun's three steps: done ones checked, the current one in ink. */
export function SetupSteps({
  steps,
  label,
}: {
  steps: SetupStepItem[];
  label: string;
}) {
  return (
    <ol className={styles.steps} aria-label={label}>
      {steps.map((step, i) => (
        <li
          key={i}
          className={`${styles.step} ${styles[step.state]}`}
          aria-current={step.state === "now" ? "step" : undefined}
        >
          <span className={styles.n} aria-hidden="true">
            {step.state === "done" ? <Icon name="check" /> : i + 1}
          </span>
          <div className={styles.body}>
            <p className={styles.stepTitle}>{step.title}</p>
            <p className={styles.stepDetail}>{step.detail}</p>
            {step.extra}
          </div>
        </li>
      ))}
    </ol>
  );
}
