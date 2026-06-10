"use client";

import { useEffect, useState, type CSSProperties, type ReactNode } from "react";
import { Moon, Sunrise, SunMedium, Sunset } from "lucide-react";
import { getHubAccentStyle, type HubAccent } from "../tokens/hubs";
import {
  AURORA_GRADIENTS,
  SIGNATURE_AURORA,
  getHubHeaderTimeBucket,
  type HubHeaderAuroraMode,
  type HubHeaderTimeBucket,
} from "../tokens/hub-aurora";
import { FAST, EASE } from "../tokens/motion";

export type HubHeaderBannerLayout = "inline-right" | "centered-notion";

// SIGNATURE_AURORA, AURORA_GRADIENTS, getHubHeaderTimeBucket,
// HubHeaderTimeBucket, and HubHeaderAuroraMode now live in
// `tokens/hub-aurora.ts` so they remain importable from server components
// without crossing this `"use client"` module's graph. Re-exported below
// for backwards compatibility with existing import paths.
export {
  AURORA_GRADIENTS,
  SIGNATURE_AURORA,
  getHubHeaderTimeBucket,
  type HubHeaderAuroraMode,
  type HubHeaderTimeBucket,
};

const CONTENT_OFFSET_TRANSITION = `transform ${FAST}s cubic-bezier(${EASE.join(", ")})`;
const HEADER_TEXT_Y = -28;
const HEADER_ICON_Y = -14;

// Banner aurora + sun-phase icon roll forward with local time without
// a page refresh. 15 minutes matches useAuroraOnRoot's cadence so the
// page-background gradient and the banner gradient flip buckets in
// lockstep within the same minute.
const BUCKET_REFRESH_MS = 15 * 60_000;

/**
 * Client-local time bucket with a 15-min refresh. SSR-safe: the lazy
 * initializer returns the server-provided fallback so the first paint
 * matches whatever the parent passed (no hydration mismatch); hydration
 * immediately replaces it with the user's local-time bucket via the
 * effect's first synchronous run.
 *
 * Why this exists: the server computes `summary.header.time_bucket`
 * from its OWN clock (`time.Now()` in the host TZ, which on Fly.io /
 * Vercel is UTC). For any non-UTC user this is wrong, and several of
 * the bucket gradients (deep-night, late-night, signature) all carry
 * violet hues — the perceptual effect is "stuck on signature." The
 * server bucket stays useful as the SSR-paintable fallback and as the
 * source for `greeting` copy (parent still derives that), but the
 * banner aurora itself must follow local time to match
 * `useAuroraOnRoot`.
 */
function useLocalTimeBucket(
  fallback: HubHeaderTimeBucket,
): HubHeaderTimeBucket {
  const [bucket, setBucket] = useState<HubHeaderTimeBucket>(() =>
    typeof window === "undefined"
      ? fallback
      : getHubHeaderTimeBucket(new Date()),
  );
  useEffect(() => {
    // Reconcile after hydration in case the SSR fallback differed from
    // the user's local time. (The lazy initializer already handled the
    // client-side mount path, so this is a no-op there.)
    setBucket(getHubHeaderTimeBucket(new Date()));
    const handle = setInterval(() => {
      setBucket(getHubHeaderTimeBucket(new Date()));
    }, BUCKET_REFRESH_MS);
    return () => clearInterval(handle);
  }, []);
  return bucket;
}

const TIME_ICON_COLOR: Record<HubHeaderTimeBucket, string> = {
  "deep-night": "oklch(0.55 0.15 265)",
  morning: "oklch(0.72 0.15 55)",
  noon: "oklch(0.82 0.14 75)",
  afternoon: "oklch(0.72 0.15 45)",
  evening: "oklch(0.65 0.17 25)",
  "late-night": "oklch(0.5 0.14 285)",
};

const TIME_ICON: Record<
  HubHeaderTimeBucket,
  { icon: typeof Moon; color: string }
> = {
  "deep-night": { icon: Moon, color: TIME_ICON_COLOR["deep-night"] },
  morning: { icon: Sunrise, color: TIME_ICON_COLOR.morning },
  noon: { icon: SunMedium, color: TIME_ICON_COLOR.noon },
  afternoon: { icon: SunMedium, color: TIME_ICON_COLOR.afternoon },
  evening: { icon: Sunset, color: TIME_ICON_COLOR.evening },
  "late-night": { icon: Moon, color: TIME_ICON_COLOR["late-night"] },
};

function HeaderSkeletonBlock({
  className,
  style,
}: {
  className: string;
  style?: CSSProperties;
}) {
  return (
    <div
      className={`animate-pulse rounded-md ${className}`}
      style={{
        background: "oklch(from var(--foreground) l c h / 0.07)",
        ...style,
      }}
    />
  );
}

function HubAvatar({
  hubType,
  accent,
  label,
  size,
  haloed = false,
}: {
  hubType: "personal" | "team";
  accent?: HubAccent | string;
  label: string;
  size: number;
  haloed?: boolean;
}) {
  // Hub avatar shape is always rounded-square regardless of hub type — hubs
  // are containers (like folders/projects/workspaces), distinct from USER
  // avatars which remain circular. Consistent with HubBadge in the
  // web package. Derek-approved.
  const radius = "rounded-surface";
  const fontSize = Math.round(size * 0.42);
  const accentStyle = getHubAccentStyle(accent, hubType);

  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      {haloed && (
        <div
          className="absolute pointer-events-none"
          style={{
            inset: -Math.round(size * 0.28),
            borderRadius: "9999px",
            background: accentStyle.halo,
            filter: "blur(8px)",
          }}
        />
      )}
      <div
        className={`relative ${radius} flex items-center justify-center font-semibold`}
        style={{
          width: size,
          height: size,
          fontSize,
          background: accentStyle.background,
          color: accentStyle.foreground,
          boxShadow: haloed
            ? "0 2px 12px oklch(0.55 0.15 290 / 0.12), inset 0 0 0 1px oklch(from var(--foreground) l c h / 0.06)"
            : "inset 0 0 0 1px oklch(from var(--foreground) l c h / 0.08)",
        }}
      >
        {label}
      </div>
    </div>
  );
}

function TeamMemberCluster({
  members,
  totalCount,
}: {
  members:
    | Array<{
        user_id: string;
        user_name: string;
        user_avatar_url?: string;
      }>
    | undefined;
  totalCount: number;
}) {
  if (!members || members.length === 0) return null;
  const visible = members.slice(0, 3);
  const extra = Math.max(totalCount - visible.length, 0);

  return (
    <div className="flex items-center">
      {visible.map((member, index) => (
        <div
          key={member.user_id}
          className="h-5 w-5 overflow-hidden rounded-full flex items-center justify-center text-[10px] font-medium"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.08)",
            color: "var(--fg-2)",
            marginLeft: index === 0 ? 0 : -6,
            border: "1.5px solid var(--background)",
            zIndex: 10 - index,
          }}
        >
          {member.user_avatar_url ? (
            <img
              src={member.user_avatar_url}
              alt=""
              className="h-full w-full"
            />
          ) : (
            (member.user_name || "?")[0]?.toUpperCase()
          )}
        </div>
      ))}
      {extra > 0 && (
        <span className="ml-1 text-[11px] text-fg-3 tabular-nums">
          +{extra}
        </span>
      )}
    </div>
  );
}

function BannerContent({
  hubName,
  hubType,
  hubAccent,
  avatarLabel,
  greeting,
  timeBucket,
  statsLine,
  members,
  memberCount,
  headerAction,
  layout,
  compact,
  auraVisible,
  contentOffset = 0,
}: {
  hubName: string;
  hubType: "personal" | "team";
  hubAccent?: HubAccent | string;
  avatarLabel: string;
  greeting: string;
  timeBucket: HubHeaderTimeBucket;
  statsLine: ReactNode;
  members?: Array<{
    user_id: string;
    user_name: string;
    user_avatar_url?: string;
  }>;
  memberCount?: number;
  /**
   * Optional control rendered at the right edge of the name row, ahead of
   * the time-of-day icon. Used by the web package to mount a team-hub
   * invite affordance for owners/admins without coupling this primitive
   * to auth or role logic.
   */
  headerAction?: ReactNode;
  layout: HubHeaderBannerLayout;
  compact: boolean;
  auraVisible: boolean;
  contentOffset?: number;
}) {
  const TimeIcon = TIME_ICON[timeBucket].icon;
  const avatarSize = compact ? 56 : layout === "centered-notion" ? 80 : 72;
  const titleSize = compact ? 20 : layout === "centered-notion" ? 28 : 24;
  const greetingSize = compact ? 13 : 15;
  const overlapAbovePx =
    layout === "centered-notion"
      ? Math.round(avatarSize * 0.5)
      : Math.round(avatarSize * 0.25);

  if (!auraVisible) {
    return (
      <div
        className={`mx-auto max-w-4xl ${compact ? "px-5 pt-8 pb-6" : "px-5 sm:px-8 pt-12 pb-8"}`}
        style={{
          transform: contentOffset
            ? `translateX(${contentOffset}px)`
            : undefined,
          transition: CONTENT_OFFSET_TRANSITION,
        }}
      >
        <div className="flex items-start gap-5">
          <HubAvatar
            hubType={hubType}
            accent={hubAccent}
            label={avatarLabel}
            size={avatarSize}
          />
          <div className="min-w-0 flex-1">
            <div className="mb-1.5 flex items-center gap-3">
              <span
                className="font-bold text-foreground"
                style={{ fontSize: titleSize, letterSpacing: "-0.02em" }}
              >
                {hubName}
              </span>
              {hubType === "team" && (
                <TeamMemberCluster
                  members={members}
                  totalCount={memberCount ?? 0}
                />
              )}
              {headerAction && (
                <div className="ml-auto flex items-center">{headerAction}</div>
              )}
            </div>
            <p
              className="mb-2 leading-relaxed text-fg-2"
              style={{ fontSize: greetingSize }}
            >
              {greeting}
            </p>
            {statsLine}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      className={`relative mx-auto max-w-4xl ${compact ? "px-5" : "px-5 sm:px-8"}`}
      style={{
        transform: contentOffset ? `translateX(${contentOffset}px)` : undefined,
        transition: CONTENT_OFFSET_TRANSITION,
      }}
    >
      {layout === "centered-notion" ? (
        <div className="flex flex-col items-start pb-2">
          <div style={{ marginTop: -overlapAbovePx + HEADER_ICON_Y }}>
            <HubAvatar
              hubType={hubType}
              accent={hubAccent}
              label={avatarLabel}
              size={avatarSize}
              haloed
            />
          </div>
          <div
            className="mt-2 w-full"
            style={{ transform: `translateY(${HEADER_TEXT_Y}px)` }}
          >
            <div className="mb-1.5 flex items-center gap-3">
              <span
                className="font-bold text-foreground"
                style={{ fontSize: titleSize, letterSpacing: "-0.02em" }}
              >
                {hubName}
              </span>
              {hubType === "team" && (
                <TeamMemberCluster
                  members={members}
                  totalCount={memberCount ?? 0}
                />
              )}
              <div className="ml-auto flex items-center gap-2.5 text-[11px] text-fg-3">
                {headerAction}
                <TimeIcon
                  className="h-3.5 w-3.5"
                  style={{ color: TIME_ICON[timeBucket].color }}
                  strokeWidth={1.8}
                />
              </div>
            </div>
            <p
              className="mb-2.5 leading-relaxed text-fg-2"
              style={{ fontSize: greetingSize }}
            >
              {greeting}
            </p>
            {statsLine}
          </div>
        </div>
      ) : (
        <div className="flex items-start gap-5 pb-2">
          <div style={{ marginTop: -overlapAbovePx + HEADER_ICON_Y }}>
            <HubAvatar
              hubType={hubType}
              accent={hubAccent}
              label={avatarLabel}
              size={avatarSize}
              haloed
            />
          </div>
          <div
            className="min-w-0 flex-1"
            style={{ transform: `translateY(${HEADER_TEXT_Y}px)` }}
          >
            <div className="mb-1.5 flex items-center gap-3">
              <span
                className="font-bold text-foreground"
                style={{ fontSize: titleSize, letterSpacing: "-0.02em" }}
              >
                {hubName}
              </span>
              {hubType === "team" && (
                <TeamMemberCluster
                  members={members}
                  totalCount={memberCount ?? 0}
                />
              )}
              <div className="ml-auto flex items-center gap-2.5 text-[11px] text-fg-3">
                {headerAction}
                <TimeIcon
                  className="h-3.5 w-3.5"
                  style={{ color: TIME_ICON[timeBucket].color }}
                  strokeWidth={1.8}
                />
              </div>
            </div>
            <p
              className="mb-2 leading-relaxed text-fg-2"
              style={{ fontSize: greetingSize }}
            >
              {greeting}
            </p>
            {statsLine}
          </div>
        </div>
      )}
    </div>
  );
}

export function HubHeaderBanner({
  hubName,
  hubType,
  hubAccent,
  avatarLabel,
  greeting,
  timeBucket,
  statsLine,
  members,
  memberCount,
  headerAction,
  aurora = "signature",
  layout = "inline-right",
  compact = false,
  height = 172,
  contentOffset = 0,
}: {
  hubName: string;
  hubType: "personal" | "team";
  hubAccent?: HubAccent | string;
  avatarLabel: string;
  greeting: string;
  timeBucket: HubHeaderTimeBucket;
  statsLine: ReactNode;
  members?: Array<{
    user_id: string;
    user_name: string;
    user_avatar_url?: string;
  }>;
  memberCount?: number;
  headerAction?: ReactNode;
  aurora?: HubHeaderAuroraMode;
  layout?: HubHeaderBannerLayout;
  compact?: boolean;
  height?: number;
  contentOffset?: number;
}) {
  // Aurora gradient + sun-phase icon follow LOCAL time, not the
  // server's UTC time_bucket. See useLocalTimeBucket — the server
  // bucket prop stays as the SSR fallback so the first paint doesn't
  // jump on hydration.
  const localBucket = useLocalTimeBucket(timeBucket);
  const effectiveAurora =
    hubType === "team" && aurora === "time" ? "signature" : aurora;
  const bannerHeight = compact ? 140 : height;
  const gradient =
    effectiveAurora === "time"
      ? AURORA_GRADIENTS[localBucket]
      : SIGNATURE_AURORA;

  if (effectiveAurora === "none") {
    return (
      <section className="relative w-full">
        <BannerContent
          hubName={hubName}
          hubType={hubType}
          hubAccent={hubAccent}
          avatarLabel={avatarLabel}
          greeting={greeting}
          timeBucket={localBucket}
          statsLine={statsLine}
          members={members}
          memberCount={memberCount}
          headerAction={headerAction}
          layout={layout}
          compact={compact}
          auraVisible={false}
          contentOffset={contentOffset}
        />
      </section>
    );
  }

  return (
    <section className="relative w-full">
      <div
        className="relative w-full overflow-hidden"
        style={{ height: bannerHeight, background: gradient }}
      >
        <div
          className="absolute inset-0 pointer-events-none opacity-[0.05]"
          style={{
            backgroundImage:
              "radial-gradient(circle at 1px 1px, var(--foreground) 0.5px, transparent 0)",
            backgroundSize: "16px 16px",
          }}
        />
        <div
          className="absolute inset-x-0 bottom-0 pointer-events-none"
          style={{
            height: Math.round(bannerHeight * 0.25),
            background:
              "linear-gradient(to bottom, transparent, var(--background))",
          }}
        />
      </div>
      <BannerContent
        hubName={hubName}
        hubType={hubType}
        hubAccent={hubAccent}
        avatarLabel={avatarLabel}
        greeting={greeting}
        timeBucket={timeBucket}
        statsLine={statsLine}
        members={members}
        memberCount={memberCount}
        headerAction={headerAction}
        layout={layout}
        compact={compact}
        auraVisible
        contentOffset={contentOffset}
      />
    </section>
  );
}

export function HubHeaderBannerSkeleton({
  hubType,
  aurora = "signature",
  compact = false,
  contentOffset = 0,
}: {
  hubType: "personal" | "team";
  aurora?: HubHeaderAuroraMode;
  compact?: boolean;
  contentOffset?: number;
}) {
  const timeBucket = getHubHeaderTimeBucket(new Date());
  const effectiveAurora =
    hubType === "team" && aurora === "time" ? "signature" : aurora;
  const bannerHeight = compact ? 140 : 172;
  const avatarSize = compact ? 56 : 72;
  const gradient =
    effectiveAurora === "time"
      ? AURORA_GRADIENTS[timeBucket]
      : SIGNATURE_AURORA;
  const overlapAbovePx = Math.round(avatarSize * 0.25);

  if (effectiveAurora === "none") {
    return (
      <div
        className={`mx-auto max-w-4xl ${compact ? "px-5 pt-8 pb-6" : "px-5 sm:px-8 pt-12 pb-8"}`}
        style={{
          transform: contentOffset
            ? `translateX(${contentOffset}px)`
            : undefined,
          transition: CONTENT_OFFSET_TRANSITION,
        }}
      >
        <div className="flex items-start gap-5">
          <div className="shrink-0 rounded-surface overflow-hidden">
            <HeaderSkeletonBlock
              className=""
              style={{
                width: avatarSize,
                height: avatarSize,
                borderRadius: "inherit",
              }}
            />
          </div>
          <div className="min-w-0 flex-1">
            <div className="mb-1.5 flex items-center gap-3">
              <HeaderSkeletonBlock className="h-7 w-48" />
            </div>
            <HeaderSkeletonBlock className="mb-2.5 h-4 w-[68%]" />
            <HeaderSkeletonBlock className="h-3 w-[52%]" />
          </div>
        </div>
      </div>
    );
  }

  return (
    <section className="relative w-full">
      <div
        className="relative w-full overflow-hidden"
        style={{ height: bannerHeight, background: gradient }}
      >
        <div
          className="absolute inset-0 pointer-events-none opacity-[0.05]"
          style={{
            backgroundImage:
              "radial-gradient(circle at 1px 1px, var(--foreground) 0.5px, transparent 0)",
            backgroundSize: "16px 16px",
          }}
        />
        <div
          className="absolute inset-x-0 bottom-0 pointer-events-none"
          style={{
            height: Math.round(bannerHeight * 0.25),
            background:
              "linear-gradient(to bottom, transparent, var(--background))",
          }}
        />
      </div>
      <div
        className={`relative mx-auto max-w-4xl ${compact ? "px-5" : "px-5 sm:px-8"}`}
        style={{
          transform: contentOffset
            ? `translateX(${contentOffset}px)`
            : undefined,
          transition: CONTENT_OFFSET_TRANSITION,
        }}
      >
        <div className="flex items-start gap-5 pb-2">
          <div style={{ marginTop: -overlapAbovePx + HEADER_ICON_Y }}>
            <div className="shrink-0 rounded-surface overflow-hidden">
              <HeaderSkeletonBlock
                className=""
                style={{
                  width: avatarSize,
                  height: avatarSize,
                  borderRadius: "inherit",
                }}
              />
            </div>
          </div>
          <div
            className="min-w-0 flex-1"
            style={{ transform: `translateY(${HEADER_TEXT_Y}px)` }}
          >
            <div className="mb-1.5 flex items-center gap-3">
              <HeaderSkeletonBlock className="h-7 w-48" />
              {hubType === "team" && (
                <HeaderSkeletonBlock className="h-5 w-16 rounded-full" />
              )}
              <div className="ml-auto">
                <HeaderSkeletonBlock className="h-3.5 w-3.5 rounded-full" />
              </div>
            </div>
            <HeaderSkeletonBlock className="mb-2.5 h-4 w-[68%]" />
            <HeaderSkeletonBlock className="h-3 w-[52%]" />
          </div>
        </div>
      </div>
    </section>
  );
}
