"use client";

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Button, Field, Icon } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { formatShortDate } from "@/lib/v2/copy";
import {
  isOnline,
  MACHINE_NAME_MAX,
  machineName,
  validMachineName,
  type MachineView,
} from "@/lib/v2/data/machines";
import { machineMeta, platformName, seenText } from "@/lib/v2/machines-copy";
import { useAgentName } from "../_lib/frame-copy";
import { useClock } from "./home-file-row";
import { linkHomeCommand } from "./link-home";
import styles from "./machines-panel.module.css";

type Mode = "idle" | "renaming" | "stopping";

/**
 * One machine of a Personal space (personal-layer design §8): its name
 * (the label too, where two share one), platform and the agents that read
 * its files, when it was last online, and Rename and Stop compiling here.
 * Both morph the row in place rather than opening a layer: Rename becomes
 * the name's field (↵ renames, Esc cancels); Stop compiling here becomes
 * the sentence saying what happens to its files, with Cancel taking the
 * focus. An unlinked machine says when, and how to link it again.
 */
export function MachineRow({
  machine,
  all,
  agents,
  variant,
  readOnly,
  pending,
  onRename,
  onStop,
}: {
  machine: MachineView;
  /** Every machine of the space, so two of the same name read apart. */
  all: readonly MachineView[];
  /** The agents that read its files (registry keys). */
  agents: readonly string[];
  variant: "side" | "full";
  readOnly: boolean;
  /** `rename` or `unlink` while that command runs for this machine. */
  pending: "rename" | "unlink" | null;
  onRename: (name: string) => Promise<boolean>;
  onStop: () => Promise<boolean>;
}) {
  const { t, locale } = useLocale();
  const p = t.ledger.machines.panel;
  const agentName = useAgentName();
  const when = useClock();
  const [mode, setMode] = useState<Mode>("idle");
  const [draft, setDraft] = useState(machine.name);
  const [invalid, setInvalid] = useState(false);
  const fieldRef = useRef<HTMLInputElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const renameRef = useRef<HTMLButtonElement>(null);
  const stopRef = useRef<HTMLButtonElement>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const back = useRef<"rename" | "stop" | null>(null);
  const name = machineName(machine, all);

  // The focus follows the row: into the field or onto Cancel, then back
  // to the button that opened it (or the row, once a machine is unlinked).
  useEffect(() => {
    if (mode === "renaming") {
      fieldRef.current?.focus();
      fieldRef.current?.select();
    } else if (mode === "stopping") cancelRef.current?.focus();
    else if (back.current === "rename") renameRef.current?.focus();
    else if (back.current === "stop") {
      (stopRef.current ?? rowRef.current)?.focus();
    }
  }, [mode]);

  const close = (from: "rename" | "stop") => {
    back.current = from;
    setMode("idle");
    setInvalid(false);
  };
  const submit = async () => {
    const next = validMachineName(draft);
    if (next === null) {
      setInvalid(true);
      return;
    }
    if (next === machine.name) {
      close("rename");
      return;
    }
    if (await onRename(next)) close("rename");
  };
  const onFieldKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      event.preventDefault();
      void submit();
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      setDraft(machine.name);
      close("rename");
    }
  };

  // An unlinked machine has no files left to name readers for.
  const meta = machine.linked
    ? machineMeta(t.ledger.machines, machine, agents, agentName, locale)
    : platformName(t.ledger.machines, machine.platform);
  const seen = machine.linked
    ? seenText(
        t.ledger.machines,
        machine.lastSeenAt,
        when,
        isOnline(machine, when.now),
      )
    : interpolate(p.unlinked, {
        date: formatShortDate(
          new Date(machine.unlinkedAt ?? machine.linkedAt),
          when.timeZone,
          when.locale,
        ),
      });

  if (mode === "renaming") {
    return (
      <div className={`${styles.row} ${styles.editing}`}>
        <Icon name="terminal" />
        <div className={styles.form}>
          <Field
            ref={fieldRef}
            label={interpolate(p.renameLabel, { machine: name })}
            value={draft}
            maxLength={MACHINE_NAME_MAX}
            autoComplete="off"
            spellCheck={false}
            hint={interpolate(p.renameHint, { label: machine.label })}
            error={invalid ? p.renameError : undefined}
            onChange={(event) => {
              setDraft(event.currentTarget.value);
              setInvalid(false);
            }}
            onKeyDown={onFieldKey}
          />
          <div className="mx-inline">
            <Button
              size="sm"
              variant="quiet"
              kbd="Esc"
              onClick={() => {
                setDraft(machine.name);
                close("rename");
              }}
            >
              {p.cancel}
            </Button>
            <Button
              size="sm"
              variant="primary"
              kbd="↵"
              pending={pending === "rename"}
              onClick={() => void submit()}
            >
              {p.renameDo}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={rowRef}
      tabIndex={machine.linked ? undefined : -1}
      className={`${styles.row} ${styles[variant]} ${machine.linked ? "" : styles.unlinked} ${mode === "stopping" ? styles.stopping : ""}`}
    >
      <Icon name="terminal" />
      <div className={styles.who}>
        <span className={styles.name}>{name}</span>
        {/* In a side panel, when it was last online ends the meta line. */}
        <span className={styles.meta}>
          {variant === "side" ? `${meta} · ${seen}` : meta}
        </span>
      </div>
      {variant === "full" ? <span className={styles.seen}>{seen}</span> : null}
      {mode === "stopping" ? (
        <div
          className={styles.confirm}
          role="group"
          aria-label={interpolate(p.stopLabel, { machine: name })}
          onKeyDown={(event) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            close("stop");
          }}
        >
          <p>{interpolate(p.confirm, { machine: name })}</p>
          <div className="mx-inline">
            <Button
              ref={cancelRef}
              size="sm"
              variant="quiet"
              kbd="Esc"
              onClick={() => close("stop")}
            >
              {p.cancel}
            </Button>
            <Button
              size="sm"
              variant="danger"
              pending={pending === "unlink"}
              onClick={async () => {
                if (await onStop()) close("stop");
              }}
            >
              {interpolate(p.confirmDo, { machine: name })}
            </Button>
          </div>
        </div>
      ) : machine.linked ? (
        readOnly ? null : (
          <div className={`mx-inline ${styles.actions}`}>
            <Button
              ref={renameRef}
              size="sm"
              variant="quiet"
              aria-label={interpolate(p.renameAria, { machine: name })}
              onClick={() => {
                setDraft(machine.name);
                setMode("renaming");
              }}
            >
              {p.rename}
            </Button>
            <Button
              ref={stopRef}
              size="sm"
              variant="quiet"
              aria-label={interpolate(p.stopLabel, { machine: name })}
              onClick={() => setMode("stopping")}
            >
              {p.stop}
            </Button>
          </div>
        )
      ) : (
        <p className={styles.again}>
          {interpolate(p.unlinkedDetail, { command: linkHomeCommand() })}
        </p>
      )}
    </div>
  );
}
