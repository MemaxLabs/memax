"use client";

import { AlertTriangle, Ban, CircleSlash } from "lucide-react";
import { useInterpolate, useLocale } from "@/i18n";
import type { HubSubscription } from "@/lib/admin-client";

// Grace window before a hub freezes, in ms — mirrors server
// model.HubGracePeriod (7 days). Kept as a local constant to avoid
// coupling the web bundle to a server constant.
const GRACE_MS = 7 * 24 * 60 * 60 * 1000;

/** State of a hub's quota relative to the grace + freeze lifecycle. */
type State =
  | { kind: "ok" }
  | { kind: "inactive"; status: string }
  | { kind: "over-limit"; daysLeft: number; since: Date }
  | { kind: "frozen"; since: Date };

function resolveState(subscription: HubSubscription | null | undefined): State {
  if (!subscription) return { kind: "ok" };
  if (subscription.status !== "active") {
    return { kind: "inactive", status: subscription.status };
  }
  if (!subscription.over_limit_since) return { kind: "ok" };
  const since = new Date(subscription.over_limit_since);
  if (Number.isNaN(since.getTime())) return { kind: "ok" };
  const elapsed = Date.now() - since.getTime();
  // Strict greater-than to match server model.IsHubFrozen
  // (now.Sub(*OverLimitSince) > HubGracePeriod). Aligns the exact
  // freeze moment across server + client.
  if (elapsed > GRACE_MS) return { kind: "frozen", since };
  const daysLeft = Math.max(
    0,
    Math.ceil((GRACE_MS - elapsed) / (24 * 60 * 60 * 1000)),
  );
  return { kind: "over-limit", daysLeft, since };
}

export function QuotaBanner({
  subscription,
  memoryCount,
  memoryLimit,
}: {
  subscription?: HubSubscription | null;
  memoryCount: number;
  memoryLimit?: number;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const copy = t.admin.hubs.detail.quota;
  const state = resolveState(subscription);

  if (state.kind === "ok") return null;

  let title: string;
  let body: string;
  let Icon = AlertTriangle;
  let tone = "bg-amber-500/10 border-amber-500/40 text-amber-400";
  if (state.kind === "frozen") {
    title = copy.frozenTitle;
    body = copy.frozenBody;
    Icon = Ban;
    tone = "bg-red-500/10 border-red-500/40 text-red-400";
  } else if (state.kind === "inactive") {
    title = copy.inactiveTitle;
    body = interpolate(copy.inactiveBody, { status: state.status });
    Icon = CircleSlash;
    tone = "bg-red-500/10 border-red-500/40 text-red-400";
  } else {
    title = copy.overLimitTitle;
    const vars = {
      current: String(memoryCount),
      limit: memoryLimit !== undefined ? String(memoryLimit) : "?",
      days: String(state.daysLeft),
    };
    body =
      state.daysLeft <= 1
        ? interpolate(copy.overLimitBodyToday, vars)
        : interpolate(copy.overLimitBody, vars);
  }

  const sinceLabel =
    state.kind === "over-limit" || state.kind === "frozen"
      ? state.since.toLocaleString()
      : undefined;

  return (
    <div
      className={`rounded-surface border p-4 mb-4 flex items-start gap-3 ${tone}`}
      role="alert"
    >
      <Icon className="h-5 w-5 shrink-0 mt-0.5" aria-hidden />
      <div className="min-w-0">
        <p className="text-[14px] font-medium">{title}</p>
        <p className="text-[13px] text-fg-2 mt-1">{body}</p>
        {sinceLabel && (
          <p className="text-[12px] text-fg-3 mt-2">
            {copy.sinceLabel}: {sinceLabel}
          </p>
        )}
      </div>
    </div>
  );
}
