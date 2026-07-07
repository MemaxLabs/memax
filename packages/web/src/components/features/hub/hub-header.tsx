"use client";

import { useMemo } from "react";
import { useInterpolate, useLocale } from "@/i18n";
import type { HubSummary } from "memax-sdk";
import {
  HubHeaderBanner,
  HubHeaderBannerSkeleton,
  getHubHeaderTimeBucket,
  type HubHeaderAuroraMode,
  type HubHeaderBannerLayout,
} from "@memaxlabs/ui";
import { buildHubHeaderStatsLine } from "./hub-header-stats";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { HubInvitePopover } from "./hub-invite-popover";

interface HubHeaderProps {
  summary: HubSummary;
  viewerName?: string;
  viewerDisplayName?: string;
  viewerRole?: string;
  auroraMode?: HubHeaderAuroraMode;
  compact?: boolean;
  contentOffset?: number;
}

interface HubHeaderUnavailableProps {
  hubId: string;
  hubName: string;
  hubIcon?: string;
  hubAccent?: string;
  hubType: "personal" | "team";
  viewerName?: string;
  viewerDisplayName?: string;
  viewerRole?: string;
  stats: {
    memories: number;
    topics: number;
    inbox?: number;
    pendingReview?: number;
    members?: number;
  };
  dreamTopics?: number;
  retryLabel?: string;
  onRetry?: () => void;
  auroraMode?: HubHeaderAuroraMode;
  compact?: boolean;
  contentOffset?: number;
}

function resolveBannerLayout(
  _state: HubSummary["header"]["state"],
): HubHeaderBannerLayout {
  // Always use inline-right. The "centered-notion" variant (icon above
  // name, left-aligned) caused the new-hub empty state to read as a
  // stacked-left block rather than the usual inline identity row, and
  // introduced a layout jump as soon as the first memory landed. Keeping
  // one layout keeps the surface stable across first_time, populated,
  // and return_after_absence states.
  return "inline-right";
}

function buildFallbackGreeting({
  hubType,
  bucket,
  viewerName,
  pendingReview,
  dreamTopics,
  t,
  interpolate,
}: {
  hubType: "personal" | "team";
  bucket: ReturnType<typeof getHubHeaderTimeBucket>;
  viewerName: string;
  pendingReview?: number;
  dreamTopics?: number;
  t: ReturnType<typeof useLocale>["t"];
  interpolate: ReturnType<typeof useInterpolate>;
}) {
  if ((pendingReview ?? 0) > 0) {
    const key =
      hubType === "team"
        ? t.hubHeader.greeting.teamReviewA
        : t.hubHeader.greeting.reviewNeededA;
    return interpolate(key, { name: viewerName, n: String(pendingReview) });
  }

  if ((dreamTopics ?? 0) > 0) {
    const key =
      bucket === "morning"
        ? t.hubHeader.greeting.morningDreamA
        : t.hubHeader.greeting.dreamDeltasA;
    return interpolate(key, { name: viewerName, n: String(dreamTopics) });
  }

  switch (bucket) {
    case "deep-night":
    case "late-night":
      return interpolate(t.hubHeader.greeting.deepNightCleanA, {
        name: viewerName,
      });
    case "morning":
      return interpolate(t.hubHeader.greeting.morningCleanA, {
        name: viewerName,
      });
    case "afternoon":
      return interpolate(t.hubHeader.greeting.afternoonCleanA, {
        name: viewerName,
      });
    case "evening":
      return interpolate(t.hubHeader.greeting.timeEveningA, {
        name: viewerName,
      });
    case "noon":
      return interpolate(t.hubHeader.greeting.timeNoonA, { name: viewerName });
    default:
      return interpolate(t.hubHeader.greeting.timeAfternoonA, {
        name: viewerName,
      });
  }
}

export function HubHeader({
  summary,
  viewerName,
  viewerDisplayName,
  viewerRole,
  auroraMode = "signature",
  compact = false,
  contentOffset = 0,
}: HubHeaderProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const displayHubName = getHubDisplayName(summary.hub, t, {
    name: viewerName,
    displayName: viewerDisplayName,
  });

  const greeting = useMemo(() => {
    const params = summary.header.greeting_params;
    // The server sends name="" for accounts without a display name —
    // the fallback is a client concern so each locale can pick a word
    // that works inside its own greeting templates (the old
    // server-side "there" leaked English into every locale).
    const withNameFallback = params?.name
      ? params
      : { ...params, name: t.hubHeader.greetingNameFallback };
    return interpolate(
      t.hubHeader.greeting[summary.header.greeting_key],
      withNameFallback,
    );
  }, [
    interpolate,
    summary.header.greeting_key,
    summary.header.greeting_params,
    t,
  ]);

  const statsLine = useMemo(
    () =>
      buildHubHeaderStatsLine({
        stats: {
          memories: summary.stats.memories,
          topics: summary.stats.topics,
          inbox: summary.stats.inbox,
          pendingReview: summary.stats.pending_review,
          members: summary.stats.members,
        },
        t,
        interpolate,
      }),
    [interpolate, summary.stats, t],
  );

  const avatarLabel = getHubDisplayInitial(summary.hub, t, {
    name: viewerName,
    displayName: viewerDisplayName,
  });
  const layout = resolveBannerLayout(summary.header.state);
  const isMobile = useIsMobile();

  // Invite affordance: only meaningful on team hubs where the viewer can
  // actually create invites. Server still enforces this via the invite
  // handler, but gating the UI avoids showing a dead-end button.
  const canInvite =
    summary.hub.hub_type === "team" &&
    (viewerRole === "owner" || viewerRole === "admin");
  const headerAction = canInvite ? (
    <HubInvitePopover hubId={summary.hub.id} hubName={displayHubName} />
  ) : undefined;

  return (
    <div className={isMobile ? undefined : "animate-content-ready"}>
      <HubHeaderBanner
        hubName={displayHubName}
        hubType={summary.hub.hub_type}
        hubAccent={summary.hub.accent}
        avatarLabel={avatarLabel}
        greeting={greeting}
        timeBucket={summary.header.time_bucket}
        statsLine={statsLine}
        members={summary.members_preview}
        memberCount={summary.stats.members}
        headerAction={headerAction}
        aurora={auroraMode}
        layout={layout}
        compact={compact}
        height={172}
        contentOffset={contentOffset}
      />
    </div>
  );
}

export function HubHeaderSkeleton({
  hubType,
  auroraMode = "signature",
  compact = false,
  contentOffset = 0,
}: {
  hubType: "personal" | "team";
  auroraMode?: HubHeaderAuroraMode;
  compact?: boolean;
  contentOffset?: number;
}) {
  return (
    <HubHeaderBannerSkeleton
      hubType={hubType}
      aurora={auroraMode}
      compact={compact}
      contentOffset={contentOffset}
    />
  );
}

export function HubHeaderUnavailable({
  hubId,
  hubName,
  hubIcon,
  hubAccent,
  hubType,
  viewerName,
  viewerDisplayName,
  viewerRole,
  stats,
  dreamTopics,
  retryLabel,
  onRetry,
  auroraMode = "signature",
  compact = false,
  contentOffset = 0,
}: HubHeaderUnavailableProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const displayHubName = getHubDisplayName(
    { name: hubName, icon: hubIcon, hub_type: hubType },
    t,
    {
      name: viewerName,
      displayName: viewerDisplayName,
    },
  );
  const avatarLabel = getHubDisplayInitial(
    { name: hubName, icon: hubIcon, hub_type: hubType },
    t,
    {
      name: viewerName,
      displayName: viewerDisplayName,
    },
  );
  const viewerLabel =
    viewerDisplayName?.trim() || viewerName?.trim() || t.hubHeader.previewName;
  const timeBucket = getHubHeaderTimeBucket(new Date());
  const greeting = buildFallbackGreeting({
    hubType,
    bucket: timeBucket,
    viewerName: viewerLabel,
    pendingReview: stats.pendingReview,
    dreamTopics,
    t,
    interpolate,
  });
  const statsLine = buildHubHeaderStatsLine({ stats, t, interpolate });
  const isMobile = useIsMobile();
  const canInvite =
    hubType === "team" && (viewerRole === "owner" || viewerRole === "admin");
  const headerAction = canInvite ? (
    <HubInvitePopover hubId={hubId} hubName={displayHubName} />
  ) : undefined;

  return (
    <div className={isMobile ? undefined : "animate-content-ready"}>
      <HubHeaderBanner
        hubName={displayHubName}
        hubType={hubType}
        hubAccent={hubAccent}
        avatarLabel={avatarLabel}
        greeting={greeting}
        timeBucket={timeBucket}
        statsLine={
          retryLabel && onRetry ? (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              {statsLine}
              <button
                onClick={onRetry}
                className="cursor-pointer text-[12px] font-medium text-fg-2 transition-colors hover:text-foreground"
              >
                {retryLabel}
              </button>
            </div>
          ) : (
            statsLine
          )
        }
        headerAction={headerAction}
        aurora={auroraMode}
        compact={compact}
        contentOffset={contentOffset}
      />
    </div>
  );
}
