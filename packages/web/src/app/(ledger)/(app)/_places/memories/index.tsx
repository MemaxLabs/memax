"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Button, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { memoriesLede } from "@/lib/v2/copy";
import type { MemoryFilter } from "@/lib/v2/data/memories";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { EmptyState } from "../../_components/empty-state";
import { useOverlays } from "../../_lib/overlays";
import { NotYetButton, PlaceColumn } from "../place";
import { useRecordsView } from "../records-view";
import { FILTERS, Record } from "./record-list";
import styles from "./memories.module.css";

/**
 * Memories (Memories.png): the whole record by section, a state filter
 * (?filter=), search over what's loaded, "Show 50 more" through the
 * cursor, and rows a keyboard can walk (↓↑). Remember is N.
 */
export function MemoriesPlace() {
  const view = useRecordsView();
  const { copy, l, overview, locale } = view;
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const asked = params.get("filter") as MemoryFilter | null;
  const filter = asked && FILTERS.includes(asked) ? asked : "all";
  const { openCommand } = useOverlays();
  const rememberKey = useKeycap("memories.remember");
  const remember = () => openCommand("remember");
  useHotkey("memories.remember", remember);

  const counts: Record<MemoryFilter, number | null> = {
    all:
      overview?.memories.kept != null
        ? overview.memories.kept +
          overview.waiting +
          (overview.memories.forgotten ?? 0)
        : null,
    waiting: overview?.waiting ?? null,
    stale: overview?.reviewFilters?.stale ?? null,
    // Memories.png draws Merged without a count.
    merged: null,
    forgotten: overview?.memories.forgotten ?? null,
  };
  const f = l.memory.list.filters;
  const label = (key: MemoryFilter) => {
    const n = counts[key];
    switch (key) {
      case "merged":
        return f.merged;
      case "all":
        return n === null ? f.allBare : interpolate(f.all, { n });
      case "waiting":
        return n === null ? f.waitingBare : interpolate(f.waiting, { n });
      case "stale":
        return n === null ? f.staleBare : interpolate(f.stale, { n });
      case "forgotten":
        return n === null ? f.forgottenBare : interpolate(f.forgotten, { n });
    }
  };

  const empty = overview !== undefined && !overview.memories.any;
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.head}
        eyebrow={view.eyebrow}
        title={copy.memories.title}
        lede={
          overview
            ? (memoriesLede(copy, locale, overview) ?? undefined)
            : undefined
        }
        actions={
          <>
            <NotYetButton variant="secondary" icon="file">
              {copy.memories.export}
            </NotYetButton>
            <Button
              variant="primary"
              icon="plus"
              kbd={rememberKey}
              onClick={remember}
            >
              {copy.memories.remember}
            </Button>
          </>
        }
      />
      {empty ? (
        <EmptyState
          title={copy.empty.memories.title}
          detail={copy.empty.memories.detail}
          action={
            <Button variant="secondary" icon="plus" onClick={remember}>
              {copy.empty.remember}
            </Button>
          }
        />
      ) : (
        <Record
          view={view}
          filter={filter}
          filterLabel={label}
          onFilter={(next) =>
            router.replace(
              `${pathname}${next === "all" ? "" : `?filter=${next}`}`,
              {
                scroll: false,
              },
            )
          }
        />
      )}
    </PlaceColumn>
  );
}
