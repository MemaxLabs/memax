"use client";

import { Button, PageHeader, useLedger } from "@memaxlabs/ledger";
import { memoriesLede } from "@/lib/v2/copy";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { EmptyState } from "../_components/empty-state";
import { useOverlays } from "../_lib/overlays";
import { NotYetButton, PlaceBody, PlaceColumn, usePlace } from "./place";
import styles from "./place.module.css";

/**
 * Memories (Memories.png): the whole record by section. The list, its
 * search and state filters arrive with epic 1.2.
 */
export function MemoriesPlace() {
  const place = usePlace();
  const { overview, copy, locale } = place;
  const { strings } = useLedger();
  const { openCommand } = useOverlays();
  const rememberKey = useKeycap("memories.remember");
  const remember = () => openCommand("remember");

  useHotkey("memories.remember", remember);

  return (
    <PlaceColumn>
      <PageHeader
        className={styles.tightHead}
        eyebrow={place.eyebrow}
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
      <PlaceBody
        place={strings.nav.places.memories}
        isEmpty={(o) => !o.memories.any}
        empty={
          <EmptyState
            title={copy.empty.memories.title}
            detail={copy.empty.memories.detail}
            action={
              <Button variant="secondary" icon="plus" onClick={remember}>
                {copy.empty.remember}
              </Button>
            }
          />
        }
      />
    </PlaceColumn>
  );
}
