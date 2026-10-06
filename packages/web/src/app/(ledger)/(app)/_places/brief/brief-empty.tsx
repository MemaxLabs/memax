"use client";

import { useState } from "react";
import { Button, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { sectionKeyOf, type BriefStructure } from "@/lib/v2/data/brief";
import { toFailure } from "@/lib/v2/data/command-error";
import type { MemoryListItem } from "@/lib/v2/data/memories";
import type { Section } from "@/lib/v2/data/types";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { EmptyState } from "../../_components/empty-state";
import { useToast } from "../../_components/toasts";
import { useAfterCompile } from "../../_lib/compile";
import { useSource } from "../../_lib/data";
import { useOverlays } from "../../_lib/overlays";
import type { RecordsView } from "../records-view";
import styles from "./brief.module.css";

const ORDER: Section[] = [
  "decisions",
  "conventions",
  "preferences",
  "open_question",
];

/** A first Brief from what's kept: one section per kind, in the record's order. */
export function firstBrief(
  title: string,
  kept: readonly Pick<MemoryListItem, "ref" | "section">[],
  headings: Record<Section, string>,
): BriefStructure {
  return {
    title,
    summary: null,
    sections: ORDER.flatMap((section) => {
      const items = kept
        .filter((m) => m.section === section)
        .map((m) => ({ ref: m.ref }));
      return items.length
        ? [{ key: sectionKeyOf(section), heading: headings[section], items }]
        : [];
    }),
  };
}

/**
 * A space with no Brief (States board: one serif sentence, one action).
 * With kept memories, Start the Brief writes the first version from
 * them, so the space's targets can compile; otherwise Remember.
 */
export function BriefEmpty({ view }: { view: RecordsView }) {
  const { space, l, copy, overview } = view;
  const source = useSource();
  const toast = useToast();
  const afterCompile = useAfterCompile(space);
  const { openCommand } = useOverlays();
  const [keys] = useState(() => new IntentKeys());
  const [pending, setPending] = useState(false);
  const canStart = Boolean(overview?.memories.any) && space.role !== "viewer";

  const start = async () => {
    if (pending) return;
    setPending(true);
    const intent = `brief.start:${space.id}`;
    try {
      const page = await source.memories.list({ space, filter: "all" });
      const kept = page.items.filter(
        (m) => m.state === "kept" || m.state === "stale",
      );
      const structure = firstBrief(space.name, kept, l.records.sections);
      const result = await source.brief.revise({
        space,
        base: null,
        structure,
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      afterCompile();
      toast({
        state: "kept",
        text: interpolate(l.brief.toast.started, { ref: result.ref }),
      });
    } catch (err) {
      toast({
        state: "proposed",
        text: interpolate(l.brief.edit.failed, {
          reason: commandReason(l.records, l.brief, toFailure(err), space.name),
        }),
      });
    } finally {
      setPending(false);
    }
  };

  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader
        className={styles.head}
        eyebrow={interpolate(copy.brief.eyebrow, { space: space.name })}
        title={copy.brief.title}
        lede={
          space.kind === "project"
            ? copy.brief.ledeProject
            : copy.brief.ledeSpace
        }
      />
      <EmptyState
        title={copy.empty.brief.title}
        detail={canStart ? l.brief.empty.startDetail : copy.empty.brief.detail}
        action={
          canStart ? (
            <Button
              variant="secondary"
              icon="brief"
              pending={pending}
              onClick={() => void start()}
            >
              {l.brief.empty.start}
            </Button>
          ) : (
            <Button
              variant="secondary"
              icon="plus"
              onClick={() => openCommand("remember")}
            >
              {copy.empty.remember}
            </Button>
          )
        }
      />
    </div>
  );
}
