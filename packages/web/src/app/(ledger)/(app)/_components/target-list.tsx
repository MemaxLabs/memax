"use client";

import { groupTargets } from "@/lib/v2/data/home-files";
import type { TargetView } from "@/lib/v2/data/targets";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useMachines } from "../_lib/machines";
import { HomeFileRow } from "./home-file-row";
import { TargetRow } from "./target-row";

/**
 * A list of compiled files as people read them: a repository's files one
 * row each (TargetRow), and a Personal space's home files once across the
 * machines they're on (HomeFileRow), which reads the space's machines for
 * why a file is behind. The Brief's "Compiled to", Today's "Compiled
 * context", a file's "Other targets", Review's "Keeping recompiles" and a
 * memory's "Reaches" all draw it.
 */
export function TargetList({
  space,
  targets,
  variant = "compact",
}: {
  space: SpaceSummary;
  targets: readonly TargetView[];
  variant?: "compact" | "plain";
}) {
  const entries = groupTargets(targets);
  const home = entries.some((e) => e.kind === "home");
  const machines = useMachines(home ? space : null);
  return entries.map((entry) =>
    entry.kind === "repo" ? (
      <TargetRow
        key={entry.target.id}
        space={space.slug}
        target={entry.target}
        variant={variant}
      />
    ) : (
      <HomeFileRow
        key={entry.group.key}
        space={space.slug}
        group={entry.group}
        machines={machines.data}
        variant={variant}
      />
    ),
  );
}
