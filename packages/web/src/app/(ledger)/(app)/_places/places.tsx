"use client";

import { notFound } from "next/navigation";
import { Button, PageHeader, useLedger } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { agentsLede } from "@/lib/v2/copy";
import { useKeycap } from "@/lib/v2/keymap/react";
import { EmptyState } from "../_components/empty-state";
import { useToast } from "../_components/toasts";
import { useOverlays } from "../_lib/overlays";
import { NotYetButton, PlaceBody, PlaceColumn, usePlace } from "./place";
import styles from "./place.module.css";

// Handoffs, Agents, Activity and Decisions: their real headers, and the
// empty state until each screen lands (epics 4.4, 1.8, 1.1, 4.2).

export function HandoffsPlace() {
  const place = usePlace();
  const { copy } = place;
  const { strings } = useLedger();
  const handoffKey = useKeycap("memory.handoff");
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.tightHead}
        eyebrow={place.eyebrow}
        title={copy.handoffs.title}
        lede={copy.handoffs.lede}
        actions={
          <NotYetButton variant="primary" icon="plus" kbd={handoffKey}>
            {copy.handoffs.new}
          </NotYetButton>
        }
      />
      <PlaceBody
        place={strings.nav.places.handoffs}
        isEmpty={(o) => !o.handoffs}
        empty={
          <EmptyState
            title={copy.empty.handoffs.title}
            detail={copy.empty.handoffs.detail}
          />
        }
      />
    </PlaceColumn>
  );
}

export function AgentsPlace() {
  const place = usePlace();
  const { space, overview, copy, locale } = place;
  const { strings } = useLedger();
  const toast = useToast();
  const command = interpolate(copy.empty.agents.command, {
    space: space.slug,
  });
  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(command);
      toast({ state: "kept", text: copy.toast.copiedCommand });
    } catch {
      toast({ state: "off", text: copy.toast.failed });
    }
  };
  return (
    <PlaceColumn>
      <PageHeader
        eyebrow={place.eyebrow}
        title={copy.agents.title}
        lede={
          overview
            ? (agentsLede(copy, locale, overview) ?? undefined)
            : undefined
        }
        actions={
          <>
            <NotYetButton variant="secondary" icon="sync">
              {copy.agents.compileAll}
            </NotYetButton>
            <NotYetButton variant="primary" icon="plus">
              {copy.agents.connect}
            </NotYetButton>
          </>
        }
      />
      <PlaceBody
        place={strings.nav.places.agents}
        isEmpty={(o) => !o.agents || o.agents.connected === 0}
        empty={
          <EmptyState
            title={copy.empty.agents.title}
            detail={copy.empty.agents.detail}
            action={
              <Button
                variant="secondary"
                icon="copy"
                onClick={() => void copyCommand()}
              >
                {copy.empty.agents.copy}
              </Button>
            }
          >
            <code className={styles.command}>
              <span className={styles.prompt} aria-hidden="true">
                ›
              </span>
              {command}
            </code>
          </EmptyState>
        }
      />
    </PlaceColumn>
  );
}

export function ActivityPlace() {
  const place = usePlace();
  const { space, copy } = place;
  const { openCommand } = useOverlays();
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.tightHead}
        eyebrow={place.eyebrow}
        title={copy.activity.title}
        lede={interpolate(copy.activity.lede, { space: space.name })}
        actions={
          <NotYetButton variant="secondary" icon="file">
            {copy.activity.export}
          </NotYetButton>
        }
      />
      <PlaceBody
        place={copy.activity.title}
        isEmpty={(o) => !o.activity.any}
        empty={
          <EmptyState
            title={copy.empty.activity.title}
            detail={interpolate(copy.empty.activity.detail, {
              space: space.name,
            })}
            action={
              <Button
                variant="secondary"
                icon="plus"
                onClick={() => openCommand("remember")}
              >
                {copy.empty.remember}
              </Button>
            }
          />
        }
      />
    </PlaceColumn>
  );
}

export function DecisionsPlace() {
  const place = usePlace();
  const { space, copy } = place;
  const { strings } = useLedger();
  // A team space's place (HANDOFF §6); elsewhere there is no such page.
  if (space.kind !== "team") notFound();
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.tightHead}
        eyebrow={place.eyebrow}
        title={copy.decisions.title}
        lede={copy.decisions.lede}
        actions={
          <>
            <NotYetButton variant="secondary" icon="file">
              {copy.decisions.export}
            </NotYetButton>
            <NotYetButton variant="primary" icon="plus">
              {copy.decisions.record}
            </NotYetButton>
          </>
        }
      />
      <PlaceBody
        place={strings.nav.places.decisions}
        isEmpty={(o) => !o.decisions}
        empty={
          <EmptyState
            title={copy.empty.decisions.title}
            detail={copy.empty.decisions.detail}
          />
        }
      />
    </PlaceColumn>
  );
}
