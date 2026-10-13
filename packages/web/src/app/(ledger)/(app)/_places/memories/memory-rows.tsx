"use client";

import type { KeyboardEvent } from "react";
import { Button, MemoryRow, Redaction } from "@memaxlabs/ledger";
import { formatShortDate } from "@/lib/v2/copy";
import type { MemoryListItem } from "@/lib/v2/data/memories";
import { useKeycap } from "@/lib/v2/keymap/react";
import { noteText } from "@/lib/v2/records-copy";
import { StatementText } from "../../_components/statement-text";
import { useOverlays } from "../../_lib/overlays";
import type { RecordsView } from "../records-view";

/** A forgotten row's bar length, steady per memory (the words are gone, so it can't be theirs). */
function barWidth(ref: string): string {
  let n = 0;
  for (const ch of ref) n = (n * 31 + ch.charCodeAt(0)) % 997;
  return `${48 + (n % 21)}%`;
}

export function memoryHref(slug: string, ref: string): string {
  return `/${encodeURIComponent(slug)}/memories/${encodeURIComponent(ref)}`;
}

/**
 * One memory as Memories.png draws it: the statement, its mark, its note
 * and its receipt. A stale one offers Verify over its rail on hover and
 * focus (V).
 */
export function MemoryListRow({
  view,
  item,
  rowRef,
}: {
  view: RecordsView;
  item: MemoryListItem;
  rowRef?: (el: HTMLElement | null) => void;
}) {
  const { l, rc, space, agentName, timeZone, locale } = view;
  const { openVerify } = useOverlays();
  const verifyKey = useKeycap("memory.verify");
  if (item.forgotten) {
    return (
      <Redaction
        ref={rowRef}
        date={formatShortDate(new Date(item.forgotten.at), timeZone, locale)}
        by={item.forgotten.by ?? undefined}
        id={item.ref}
        detail={item.forgotten.detail ?? undefined}
        width={barWidth(item.ref)}
      />
    );
  }
  const stamp = item.receipt ? view.stamp(item.receipt.by) : null;
  return (
    <MemoryRow
      ref={rowRef}
      state={item.state === "forgotten" ? "kept" : item.state}
      unconfirmed={item.state === "proposed" || item.state === "conflict"}
      {...(stamp ?? {})}
      action={
        item.receipt ? rc.verbs[item.receipt.action] : l.records.verbs.kept
      }
      time={item.receipt ? view.time(item.receipt.at) : undefined}
      id={item.ref}
      source={item.source ?? undefined}
      note={
        item.note
          ? noteText(rc, item.note, agentName, timeZone, locale)
          : undefined
      }
      href={memoryHref(space.slug, item.ref)}
      actions={
        item.state === "stale" ? (
          <Button
            variant="secondary"
            size="sm"
            kbd={verifyKey}
            onClick={() => openVerify(item.ref)}
          >
            {l.review.flag.verify}
          </Button>
        ) : undefined
      }
    >
      <StatementText text={item.statement} />
    </MemoryRow>
  );
}

/**
 * ↓↑ (and Home, End) move between the rows' links, so the record reads
 * by keyboard like Review's queue. A list handler, not a global key:
 * it only acts while focus is on a row.
 */
export function onRowKeys(event: KeyboardEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  if (!target.classList.contains("mx-row-link")) return;
  const links = [
    ...event.currentTarget.querySelectorAll<HTMLElement>(".mx-row-link"),
  ];
  const i = links.indexOf(target);
  let next: HTMLElement | undefined;
  if (event.key === "ArrowDown") next = links[i + 1];
  else if (event.key === "ArrowUp") next = links[i - 1];
  else if (event.key === "Home") next = links[0];
  else if (event.key === "End") next = links[links.length - 1];
  else return;
  event.preventDefault();
  next?.focus();
}
