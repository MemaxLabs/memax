"use client";

import { usePathname } from "next/navigation";
import { Button } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { placeHref } from "@/lib/v2/places";
import { StatusPage } from "../../_components/status-page";
import { useOverlays } from "../_lib/overlays";

// Not found and error inside the frame (States2 "Not found": say why it
// might be gone, then offer a way on). The rail stays, so the person
// keeps their bearings.

/** The URL names a space this person can't open. */
export function SpaceNotFound({
  slug,
  fallback,
}: {
  slug: string;
  fallback: SpaceSummary;
}) {
  const { t } = useLocale();
  const copy = t.ledger.app.spaces.notFound;
  return (
    <StatusPage
      variant="sheet"
      receipt={`/${slug}`}
      title={interpolate(copy.title, { slug })}
      description={copy.description}
      actions={
        <Button variant="secondary" href={placeHref(fallback.slug, "today")}>
          {interpolate(copy.open, { space: fallback.name })}
        </Button>
      }
    />
  );
}

/** notFound() inside a space: a page or a record that isn't there. */
export function PlaceNotFound({ space }: { space: SpaceSummary }) {
  const { t } = useLocale();
  const pathname = usePathname();
  const { openCommand } = useOverlays();
  return (
    <StatusPage
      variant="sheet"
      receipt={pathname ?? undefined}
      title={t.ledger.notFound.title}
      description={t.ledger.notFound.description}
      actions={
        <>
          <Button
            variant="secondary"
            size="sm"
            icon="search"
            onClick={() => openCommand("ask")}
          >
            {t.ledger.app.notFound.searchMemories}
          </Button>
          <Button
            variant="quiet"
            size="sm"
            href={placeHref(space.slug, "activity")}
          >
            {t.ledger.app.notFound.openActivity}
          </Button>
        </>
      }
    />
  );
}

/** A page inside a space failed to render. */
export function PlaceError({
  space,
  digest,
  onRetry,
}: {
  space: SpaceSummary | undefined;
  digest?: string;
  onRetry: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.error;
  return (
    <StatusPage
      variant="sheet"
      role="alert"
      receipt={digest ? interpolate(copy.digest, { digest }) : undefined}
      title={copy.title}
      description={copy.description}
      actions={
        <>
          <Button variant="primary" size="sm" icon="sync" onClick={onRetry}>
            {copy.retry}
          </Button>
          {space ? (
            <Button
              variant="quiet"
              size="sm"
              href={placeHref(space.slug, "today")}
            >
              {t.ledger.app.error.today}
            </Button>
          ) : null}
        </>
      }
    />
  );
}
