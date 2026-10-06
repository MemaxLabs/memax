"use client";

import { Button, PageHeader, useLedger } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatClock } from "@/lib/v2/copy";
import { EmptyState } from "../_components/empty-state";
import { useOverlays } from "../_lib/overlays";
import { PlaceBody, usePlace } from "./place";
import styles from "./place.module.css";

/**
 * The Brief (Brief.png): the living document every agent reads. Its
 * facts in the ruled margin and its side panels (compiled to, reads,
 * sources) arrive with epic 1.5.
 */
export function BriefPlace() {
  const { space, overview, copy, locale, timeZone } = usePlace();
  const { strings } = useLedger();
  const { openCommand } = useOverlays();
  const brief = overview?.brief ?? null;
  const b = copy.brief;

  const eyebrow = [
    interpolate(b.eyebrow, { space: space.name }),
    brief?.rewrittenAt
      ? interpolate(b.rewritten, {
          time: formatClock(brief.rewrittenAt, timeZone, locale),
        })
      : null,
    brief?.facts ? count(b.factsOne, b.facts, brief.facts) : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className={`mx-page ${styles.wide}`}>
      <div className={styles.briefGrid}>
        <article>
          <PageHeader
            className={styles.briefHead}
            eyebrow={eyebrow}
            title={brief?.title ?? b.title}
            lede={space.kind === "project" ? b.ledeProject : b.ledeSpace}
          />
          <PlaceBody
            place={strings.nav.places.briefs}
            isEmpty={(o) => !o.brief || o.brief.facts === 0 || !o.memories.any}
            empty={
              <EmptyState
                title={copy.empty.brief.title}
                detail={copy.empty.brief.detail}
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
        </article>
      </div>
    </div>
  );
}
