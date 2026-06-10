"use client";

/**
 * V2HubRoute — shared gate for shell-v2 routes under `/h/[slug]/...`.
 *
 * Resolves the URL slug to the active hub via `useV2RouteHubSync` and
 * dispatches one of three surfaces:
 *
 *   - `isAuthLoading=true`            — render null (parent layout
 *                                       already provides skeleton chrome).
 *   - `isAuthLoading=false &&
 *      resolvedHubId===null`          — render `<HubNotFound>` (the slug
 *                                       doesn't match any hub the user
 *                                       belongs to — typo or revoked
 *                                       access).
 *   - `isSynced=true`                 — render route children. Without
 *                                       the sync gate components like
 *                                       `<TopicGrid>` / `<TopicDetail>`
 *                                       (which read `useActiveHub()`)
 *                                       would briefly render data from
 *                                       the previous active hub under
 *                                       the new URL during the
 *                                       mount-to-switchHub window.
 */

import { type ReactNode } from "react";
import Link from "next/link";
import { useLocale } from "@/i18n";
import { useV2RouteHubSync } from "@/hooks/use-v2-route-hub-sync";

interface V2HubRouteProps {
  slug: string;
  children: ReactNode;
}

export function V2HubRoute({ slug, children }: V2HubRouteProps) {
  const { resolvedHubId, isAuthLoading, isSynced } = useV2RouteHubSync(slug);
  if (isAuthLoading) return null;
  if (resolvedHubId === null) return <HubNotFound />;
  if (!isSynced) return null;
  return <>{children}</>;
}

function HubNotFound() {
  const { t } = useLocale();
  return (
    <div className="mx-auto flex max-w-md flex-col items-center gap-3 px-6 py-24 text-center">
      <h1 className="text-[18px] font-semibold text-fg-1">
        {t.hubRoute.notFound.title}
      </h1>
      <p className="text-[14px] text-fg-3">{t.hubRoute.notFound.description}</p>
      <Link
        href="/h/personal/memories"
        className="mt-2 rounded-md border border-border/60 px-3 py-1.5 text-[13px] text-fg-2 transition-colors hover:bg-surface-1"
      >
        {t.hubRoute.notFound.backToPersonal}
      </Link>
    </div>
  );
}
