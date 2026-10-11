"use client";

import { useId } from "react";
import { Button } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { machineAgents } from "@/lib/v2/data/home-files";
import { orderMachines } from "@/lib/v2/data/machines";
import type { TargetView } from "@/lib/v2/data/targets";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { linkedText } from "@/lib/v2/machines-copy";
import { useMachineCommands, useMachines } from "../_lib/machines";
import { LinkHome } from "./link-home";
import { MachineRow } from "./machine-row";
import styles from "./machines-panel.module.css";

/**
 * The machines a Personal space is written to (personal-layer design
 * §8), on its Brief beside "Compiled to" (`side`) and in Settings ›
 * Account (`full`): each machine with what it is, who reads its files
 * and when it was last online, Rename and Stop compiling here. With
 * none linked, the command that links one; in Settings, the same command
 * under the list, for the next computer.
 */
export function MachinesPanel({
  space,
  targets,
  variant,
  title,
}: {
  space: SpaceSummary;
  /** The space's targets, for the agents that read each machine's files. */
  targets: readonly TargetView[] | undefined;
  variant: "side" | "full";
  /** The panel's title, where "Machines" alone wouldn't say whose. */
  title?: string;
}) {
  const { t } = useLocale();
  const m = t.ledger.machines;
  const id = useId();
  const machines = useMachines(space);
  const commands = useMachineCommands(space);
  const list = orderMachines(machines.data ?? []);
  const linked = list.filter((x) => x.linked).length;
  const readOnly = space.role !== "owner";

  return (
    <section className="mx-panel" aria-labelledby={id}>
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id={id}>
          {title ?? m.panel.title}
        </h2>
        {machines.data ? (
          <span className="mx-meta">{linkedText(m, linked)}</span>
        ) : null}
      </header>
      {machines.isError && !machines.data ? (
        <div className={styles.alert} role="alert">
          <span>{m.panel.loadFailed}</span>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void machines.refetch()}
          >
            {m.panel.retry}
          </Button>
        </div>
      ) : null}
      {list.map((machine) => (
        <MachineRow
          key={machine.id}
          machine={machine}
          all={list}
          agents={machineAgents(machine, targets ?? [])}
          variant={variant}
          readOnly={readOnly}
          pending={
            commands.pending === `rename:${machine.id}`
              ? "rename"
              : commands.pending === `unlink:${machine.id}`
                ? "unlink"
                : null
          }
          onRename={(name) => commands.rename(machine, name, list)}
          onStop={() => commands.unlink(machine, list)}
        />
      ))}
      {machines.data && linked === 0 ? (
        <LinkHome lead={m.panel.none} />
      ) : machines.data && variant === "full" ? (
        <LinkHome lead={m.panel.another} />
      ) : null}
    </section>
  );
}
