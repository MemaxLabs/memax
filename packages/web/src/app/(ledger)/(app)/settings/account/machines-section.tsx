"use client";

import { useLocale } from "@/i18n";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { MachinesPanel } from "../../_components/machines-panel";
import { useTargets } from "../../_lib/compile";
import { usePersonalSpace } from "../../_lib/machines";

/**
 * Settings › Account's machines (personal-layer design §8): the
 * computers the person's Personal space is written to, beside where
 * they're signed in, with Rename and Stop compiling here. Nothing for
 * someone whose Personal space isn't on the V2 record.
 */
export function MachinesSection() {
  const personal = usePersonalSpace();
  return personal ? <Machines space={personal} /> : null;
}

function Machines({ space }: { space: SpaceSummary }) {
  const { t } = useLocale();
  const targets = useTargets(space);
  return (
    <MachinesPanel
      space={space}
      targets={targets.data}
      variant="full"
      title={t.ledger.machines.panel.titleAccount}
    />
  );
}
