"use client";

import { useCallback, useState } from "react";
import Link from "next/link";
import { Button, StateMark } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import {
  knownMachines,
  machineCopies,
  siblingsOf,
} from "@/lib/v2/data/home-files";
import { machineName, type MachineView } from "@/lib/v2/data/machines";
import type { TargetView } from "@/lib/v2/data/targets";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { machineStateText, type MachinesCopy } from "@/lib/v2/machines-copy";
import { linkHomeCommand } from "../../_components/link-home";
import { targetHref } from "../../_components/target-row";
import { useToast } from "../../_components/toasts";
import { useAfterCompile } from "../../_lib/compile";
import { useSource } from "../../_lib/data";
import { useMachines } from "../../_lib/machines";
import type { RecordsView } from "../records-view";
import { useTargetSettings } from "./use-target-settings";
import styles from "./target.module.css";

// One machine's copy of a Personal space's home file, on its page
// (TargetPreview, personal-layer design §8): which machine, the switch to
// the other machines' copies, Stop compiling here, and what a stopped
// copy or an unlinked machine means.

/** The machine a home target is on, by the name the person sees; nulls for a repository's file. */
export function useMachineOf(view: RecordsView, target: TargetView) {
  const machines = useMachines(target.machine ? view.space : null);
  const machine = machines.data?.find((m) => m.id === target.machine?.id);
  const name = target.machine
    ? machineName(target.machine, knownMachines([target], machines.data))
    : null;
  return { name, machine, machines: machines.data };
}

/** What a home file's page says it is, for its kind. */
export function homeLede(
  m: MachinesCopy,
  target: TargetView,
  machine: string,
): string {
  const lede = m.target.lede;
  const template =
    target.kind === "claude_user"
      ? lede.claude_user
      : target.kind === "codex_user"
        ? lede.codex_user
        : target.kind === "gemini_user"
          ? lede.gemini_user
          : target.kind === "copilot_user"
            ? lede.copilot_user
            : target.kind === "windsurf_user"
              ? lede.windsurf_user
              : lede.agents_md;
  const text = interpolate(template, { machine, file: target.label });
  if (target.kind !== "agents_md") return text;
  // The agents with no file of their own that read the canonical copy.
  const readers = (["opencode", "cursor"] as const).filter((a) =>
    target.readBy.includes(a),
  );
  const r = m.target.canonicalReaders;
  return [text, ...readers.map((a) => r[a])].join(r.gap);
}

/**
 * The machines this file is on, as links to each one's copy (a nav, so
 * each is a page of its own): the current one marked, each with its
 * state's glyph and word. With one machine, the machine's name alone.
 */
export function MachineSwitcher({
  view,
  target,
  all,
  machines,
}: {
  view: RecordsView;
  target: TargetView;
  all: readonly TargetView[];
  machines: readonly MachineView[] | undefined;
}) {
  const m = view.l.machines;
  const siblings = siblingsOf(target, all);
  const group = {
    key: target.id,
    kind: target.kind,
    path: target.path,
    label: target.label,
    agent: null,
    targets: siblings,
  };
  const copies = machineCopies(group, machines, view.now);
  if (copies.length <= 1) {
    const only = copies[0];
    return only ? (
      <span>{interpolate(m.target.on, { machine: only.name })}</span>
    ) : null;
  }
  return (
    <nav
      aria-label={m.target.switcher}
      className={`mx-seg mx-seg--sm ${styles.switcher}`}
    >
      {copies.map((copy) => {
        const here = copy.target.id === target.id;
        const state = machineStateText(m, copy.state);
        return (
          <Link
            key={copy.target.id}
            href={targetHref(view.space.slug, copy.target)}
            aria-current={here ? "page" : undefined}
            aria-label={interpolate(m.target.switcherItem, {
              machine: copy.name,
              state: state.label,
            })}
            className={`mx-seg-opt ${here ? "is-on" : ""} ${styles.machine}`}
            title={state.label}
          >
            <StateMark state={state.state} label={false} />
            {copy.name}
          </Link>
        );
      })}
    </nav>
  );
}

/**
 * Stop compiling here: this file, on this machine only (PATCH enabled
 * false, If-Match, one key per intent). The daemon there takes it off the
 * next time it's online, unless it was edited by hand.
 */
export function useStopHere(
  view: RecordsView,
  target: TargetView,
  machine: string,
) {
  const source = useSource();
  const { t } = useLocale();
  const toast = useToast();
  const afterCompile = useAfterCompile(view.space);
  const [keys] = useState(() => new IntentKeys());
  const [pending, setPending] = useState(false);
  const m = t.ledger.machines;
  const stop = useCallback(async () => {
    if (pending) return;
    const intent = intentOf("stop-here", target.id, target.version);
    const vars = { file: target.label, machine };
    setPending(true);
    try {
      const next = await source.targets.configure({
        space: view.space,
        target,
        change: { enabled: false },
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      afterCompile(next);
      toast({ state: "off", text: interpolate(m.toast.stoppedFile, vars) });
    } catch (err) {
      const failure = toFailure(err);
      if (!isRetryable(failure)) keys.settle(intent);
      afterCompile();
      toast({
        state: "proposed",
        text: interpolate(m.toast.stopFileFailed, {
          ...vars,
          reason: commandReason(
            t.ledger.records,
            t.ledger.brief,
            failure,
            view.space.name,
          ),
        }),
      });
    } finally {
      setPending(false);
    }
  }, [
    afterCompile,
    keys,
    m,
    machine,
    pending,
    source,
    t,
    target,
    toast,
    view.space,
  ]);
  return { stop, pending };
}

/**
 * A stopped copy: agents on that machine read Personal over MCP, and
 * Compile it again turns it back on. On an unlinked machine nothing is
 * written until it's linked again, so it says how instead.
 */
export function HomeStopped({
  view,
  target,
  machine,
  name,
}: {
  view: RecordsView;
  target: TargetView;
  machine: MachineView | undefined;
  name: string;
}) {
  const m = view.l.machines;
  const settings = useTargetSettings(view.space, target);
  if (machine && !machine.linked) {
    return (
      <section className="mx-panel">
        <p className={styles.note}>
          {interpolate(m.target.unlinked, {
            machine: name,
            command: linkHomeCommand(),
          })}
        </p>
      </section>
    );
  }
  return (
    <section className="mx-panel">
      <p className={styles.note}>
        {interpolate(m.target.off, { machine: name })}
      </p>
      {view.space.role === "owner" ? (
        <div className={styles.noteActions}>
          <Button
            variant="secondary"
            size="sm"
            icon="sync"
            pending={settings.pending}
            onClick={() => void settings.change({ enabled: true })}
          >
            {view.l.brief.target.restart}
          </Button>
        </div>
      ) : null}
    </section>
  );
}
