"use client";

import { interpolate, useLocale } from "@/i18n";
import { joinList } from "@/lib/v2/copy";
import { machineAgents } from "@/lib/v2/data/home-files";
import { latestLinked, machineName } from "@/lib/v2/data/machines";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { LinkHome } from "../(app)/_components/link-home";
import { useTargets } from "../(app)/_lib/compile";
import { useAgentName } from "../(app)/_lib/frame-copy";
import { useMachines, usePersonalSpace } from "../(app)/_lib/machines";
import styles from "./compile-done.module.css";

/**
 * CompileDone's line about Personal (personal-layer design §8): when
 * `memax init` linked this machine's home, what it wrote there ("Personal:
 * written for Claude Code and Codex on work-mbp"), named by the machine
 * linked last, which on the first run is this one; otherwise the command
 * that does it. Nothing for someone without a Personal space on the V2
 * record, or when the space being set up is Personal itself.
 */
export function PersonalLine({ space }: { space: SpaceSummary }) {
  const { t, locale } = useLocale();
  const s = t.ledger.machines.setup;
  const agentName = useAgentName();
  const personal = usePersonalSpace();
  const shown = personal && personal.id !== space.id ? personal : null;
  const machines = useMachines(shown);
  const targets = useTargets(shown ?? space);
  if (!shown || !machines.data) return null;
  const machine = latestLinked(machines.data);
  if (!machine) return <LinkHome lead={s.notYet} />;
  const agents = machineAgents(machine, targets.data ?? []);
  const name = machineName(machine, machines.data);
  return (
    <p className={styles.personal}>
      {agents.length
        ? interpolate(s.written, {
            agents: joinList(agents.map(agentName), locale),
            machine: name,
          })
        : interpolate(s.writtenNone, { machine: name })}
    </p>
  );
}
