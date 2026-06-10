"use client";

import { use } from "react";
import { useState } from "react";
import Link from "next/link";
import { ArrowLeft, ArrowRight, AlertTriangle } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import {
  useAdminUserDetail,
  useSetUserPlan,
  useAdminPlans,
} from "@/hooks/use-admin-users";
import {
  useAdminUserOrphanStatus,
  useReconcileOrphans,
} from "@/hooks/use-admin-orphans";

export default function AdminUserDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { t } = useLocale();
  const { data: detail, isLoading } = useAdminUserDetail(id);
  const { data: plans } = useAdminPlans();
  const setPlanMutation = useSetUserPlan();
  const [showPlanSelect, setShowPlanSelect] = useState(false);

  if (isLoading) {
    return (
      <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-4xl">
        <div className="animate-pulse space-y-4">
          <div className="h-6 w-48 bg-surface-2 rounded" />
          <div className="h-40 bg-surface-2 rounded-surface" />
          <div className="h-32 bg-surface-2 rounded-surface" />
        </div>
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-4xl">
        <p className="text-fg-3">{t.admin.users.detail.notFound}</p>
      </div>
    );
  }

  const {
    user,
    plan,
    usage,
    limits,
    hubs,
    memory_count,
    override,
    effective_plan,
    effective_plan_def,
    effective_limits,
    hub_subscriptions,
  } = detail;
  const displayName = user.display_name || user.name || user.email;

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-4xl">
      {/* Back link */}
      <Link
        href="/admin/users"
        className="inline-flex items-center gap-1.5 text-[13px] text-fg-3 hover:text-fg-2 mb-6 transition-colors"
      >
        <ArrowLeft className="h-3.5 w-3.5" />
        {t.admin.users.title}
      </Link>

      {/* Orphan status card — rendered only when this user's
          registered-without-invite-consume state is detected. Keeps
          the happy-path unchanged; appears between the back link and
          the profile card so it's the first thing an operator sees
          when opening the page. */}
      <OrphanStatusCard userId={id} />

      {/* Profile card */}
      <div className="rounded-surface border border-border/50 bg-card p-6 mb-4">
        <div className="flex items-start gap-4">
          {user.avatar_url ? (
            <img
              src={user.avatar_url}
              alt=""
              className="h-14 w-14 rounded-full bg-surface-2"
            />
          ) : (
            <div className="h-14 w-14 rounded-full bg-surface-2 flex items-center justify-center text-[18px] text-fg-3 font-medium">
              {displayName.charAt(0).toUpperCase()}
            </div>
          )}
          <div className="flex-1 min-w-0">
            <h1 className="text-[18px] font-semibold text-fg-1 truncate">
              {displayName}
            </h1>
            <p className="text-[14px] text-fg-3 truncate">{user.email}</p>
            <p className="text-[12px] text-fg-4 mt-1">
              {t.admin.users.detail.id}: {user.id} &middot;{" "}
              {t.admin.users.detail.joined}{" "}
              {user.created_at
                ? new Date(user.created_at).toLocaleDateString()
                : ""}
              {effective_plan &&
                effective_plan.plan_id !==
                  (user.personal_plan_id || user.plan) && (
                  <span className="ml-2 text-emerald-400">
                    &middot; {t.admin.hubs.effective}: {effective_plan.plan_id}{" "}
                    ({effective_plan.source})
                  </span>
                )}
            </p>
          </div>
        </div>
      </div>

      {/* Plan card */}
      <div className="rounded-surface border border-border/50 bg-card p-6 mb-4">
        <h2 className="text-[14px] font-medium text-fg-2 mb-3">
          {t.admin.users.detail.plan}
        </h2>
        <div className="flex items-center gap-3">
          {showPlanSelect ? (
            (() => {
              const personalPlans = (plans ?? []).filter(
                (p) => !("scope" in p) || p.scope === "personal",
              );
              return (
                <Select
                  value={user.personal_plan_id || user.plan}
                  onValueChange={(v: string) => {
                    setPlanMutation.mutate(
                      { userId: user.id, planId: v },
                      { onSettled: () => setShowPlanSelect(false) },
                    );
                  }}
                  items={Object.fromEntries(
                    personalPlans.map((p) => [p.id, p.display_name]),
                  )}
                >
                  <SelectTrigger className="w-56" />
                  <SelectContent>
                    {personalPlans.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.display_name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              );
            })()
          ) : (
            <>
              <span className="inline-block rounded-md bg-surface-2 px-3 py-1 text-[14px] font-medium text-fg-1">
                {plan?.display_name ?? user.personal_plan_id ?? user.plan}
              </span>
              <button
                onClick={() => setShowPlanSelect(true)}
                className="text-[13px] text-fg-3 hover:text-fg-1 transition-colors"
              >
                {t.admin.users.actions.changePlan}
              </button>
            </>
          )}
        </div>
        {limits && (
          <div className="mt-4 grid grid-cols-2 sm:grid-cols-4 gap-3 text-[13px]">
            <LimitItem
              label={t.admin.users.detail.memory}
              value={memory_count}
              limit={limits.memory_limit}
            />
            <LimitItem
              label={t.admin.users.detail.pushPerMonth}
              value={usage?.push_count ?? 0}
              limit={limits.push_limit}
            />
            <LimitItem
              label={t.admin.users.detail.recallPerMonth}
              value={usage?.recall_count ?? 0}
              limit={limits.recall_limit}
            />
            <LimitItem
              label={t.admin.users.detail.askPerMonth}
              value={usage?.ask_count ?? 0}
              limit={limits.ask_limit}
            />
          </div>
        )}
      </div>

      {/* Effective plan card — populated only when effective > personal */}
      {effective_limits && effective_plan && (
        <div className="rounded-surface border border-emerald-500/30 bg-emerald-500/5 p-6 mb-4">
          <div className="flex items-baseline justify-between gap-3 mb-1">
            <h2 className="text-[14px] font-medium text-fg-1">
              {t.admin.users.detail.effectivePlan}
            </h2>
            <span className="inline-flex items-center gap-2 text-[12px] text-fg-3">
              <span className="inline-block rounded-md bg-emerald-500/15 text-emerald-400 px-2 py-0.5 text-[12px] font-medium">
                {effective_plan_def?.display_name ?? effective_plan.plan_id}
              </span>
              <span>
                {t.admin.users.detail.effectiveSource}:{" "}
                <EffectiveSource source={effective_plan.source} hubs={hubs} />
              </span>
            </span>
          </div>
          <p className="text-[12px] text-fg-3 mb-4">
            {t.admin.users.detail.effectivePlanDescription}
          </p>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-[13px]">
            <LimitItem
              label={t.admin.users.detail.memory}
              value={memory_count}
              limit={effective_limits.memory_limit}
            />
            <LimitItem
              label={t.admin.users.detail.pushPerMonth}
              value={usage?.push_count ?? 0}
              limit={effective_limits.push_limit}
            />
            <LimitItem
              label={t.admin.users.detail.recallPerMonth}
              value={usage?.recall_count ?? 0}
              limit={effective_limits.recall_limit}
            />
            <LimitItem
              label={t.admin.users.detail.askPerMonth}
              value={usage?.ask_count ?? 0}
              limit={effective_limits.ask_limit}
            />
          </div>
        </div>
      )}

      {/* Usage card (when no limits available) */}
      {!limits && usage && (
        <div className="rounded-surface border border-border/50 bg-card p-6 mb-4">
          <h2 className="text-[14px] font-medium text-fg-2 mb-3">
            {t.admin.users.detail.usage}
          </h2>
          <div className="flex gap-6 text-[14px] text-fg-2 tabular-nums">
            <span>
              {usage.push_count} {t.admin.users.detail.pushes}
            </span>
            <span>
              {usage.recall_count} {t.admin.users.detail.recalls}
            </span>
            <span>
              {usage.ask_count} {t.admin.users.detail.asks}
            </span>
          </div>
        </div>
      )}

      {/* Overrides card */}
      {override && Object.keys(override.overrides).length > 0 && (
        <div className="rounded-surface border border-border/50 bg-card p-6 mb-4">
          <h2 className="text-[14px] font-medium text-fg-2 mb-3">
            {t.admin.users.detail.overrides}
          </h2>
          <pre className="text-[12px] text-fg-3 bg-surface-1 rounded-lg p-3 overflow-auto">
            {JSON.stringify(override.overrides, null, 2)}
          </pre>
          {override.reason && (
            <p className="text-[12px] text-fg-4 mt-2">
              {t.admin.users.detail.reason}: {override.reason}
            </p>
          )}
        </div>
      )}

      {/* Hubs card */}
      {hubs && hubs.length > 0 && (
        <div className="rounded-surface border border-border/50 bg-card p-6 mb-4">
          <h2 className="text-[14px] font-medium text-fg-2 mb-3">
            {t.admin.users.detail.hubs}
          </h2>
          <div className="space-y-2">
            {hubs.map((h) => {
              const linkable = h.hub.hub_type === "team";
              const content = (
                <>
                  <span className="text-fg-1 truncate">{h.hub.name}</span>
                  <span className="text-fg-3 capitalize ml-3 shrink-0">
                    {h.role}
                  </span>
                </>
              );
              return linkable ? (
                <Link
                  key={h.hub.id}
                  href={`/admin/hubs/${h.hub.id}`}
                  className="flex items-center justify-between text-[13px] px-2 py-1 -mx-2 rounded hover:bg-surface-1 transition-colors"
                >
                  {content}
                </Link>
              ) : (
                <div
                  key={h.hub.id}
                  className="flex items-center justify-between text-[13px]"
                >
                  {content}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Hub subscriptions card (hubs this user pays for) */}
      {hub_subscriptions && hub_subscriptions.length > 0 && (
        <div className="rounded-surface border border-border/50 bg-card p-6">
          <h2 className="text-[14px] font-medium text-fg-2 mb-3">
            {t.admin.hubs.subscription}
          </h2>
          <div className="space-y-3">
            {hub_subscriptions.map((sub) => (
              <div
                key={sub.id}
                className="flex items-center justify-between text-[13px] p-3 rounded-lg bg-surface-1"
              >
                <div className="min-w-0">
                  {sub.hub_name ? (
                    <Link
                      href={`/admin/hubs/${sub.hub_id}`}
                      className="text-fg-1 font-medium hover:underline underline-offset-2 truncate block"
                    >
                      {sub.hub_name}
                      {sub.hub_slug ? (
                        <span className="text-fg-3 font-normal">
                          {" "}
                          ({sub.hub_slug})
                        </span>
                      ) : null}
                    </Link>
                  ) : (
                    // Rare fallback: the denormalize join found no
                    // matching hub row. Still link by id so the
                    // operator has a path forward even for a stale
                    // subscription.
                    <Link
                      href={`/admin/hubs/${sub.hub_id}`}
                      className="text-fg-1 font-medium hover:underline underline-offset-2"
                    >
                      {t.admin.hubs.hubLabel}: {sub.hub_id.slice(0, 8)}…
                    </Link>
                  )}
                  <p className="text-fg-3">
                    {sub.plan_id} &middot; {sub.seat_count}{" "}
                    {t.admin.hubs.seatCount}
                  </p>
                </div>
                <span
                  className={`inline-block rounded-md px-2 py-0.5 text-[11px] font-medium ${
                    sub.status === "active"
                      ? "bg-emerald-500/15 text-emerald-400"
                      : "bg-amber-500/15 text-amber-400"
                  }`}
                >
                  {t.admin.hubs.status[
                    sub.status as keyof typeof t.admin.hubs.status
                  ] ?? sub.status}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function LimitItem({
  label,
  value,
  limit,
}: {
  label: string;
  value: number;
  limit: number;
}) {
  const isUnlimited = limit < 0;
  const percentage = isUnlimited ? 0 : limit > 0 ? (value / limit) * 100 : 0;
  const isNearLimit = percentage >= 80;

  return (
    <div>
      <p className="text-fg-3 mb-1">{label}</p>
      <p className="text-fg-1 font-medium tabular-nums">
        {value.toLocaleString()}
        <span className="text-fg-4 font-normal">
          {" "}
          / {isUnlimited ? "\u221e" : limit.toLocaleString()}
        </span>
      </p>
      {!isUnlimited && limit > 0 && (
        <div className="mt-1 h-1 rounded-full bg-surface-2 overflow-hidden">
          <div
            className={`h-full rounded-full transition-all ${isNearLimit ? "bg-amber-500" : "bg-blue-500"}`}
            style={{ width: `${Math.min(percentage, 100)}%` }}
          />
        </div>
      )}
    </div>
  );
}

/**
 * OrphanStatusCard — shown on /admin/users/{id} when this user
 * registered without their waitlist invite being consumed (the
 * 2026-04-14 → 2026-04-20 regression cohort). Renders nothing
 * for the common case; the operator sees the card only when
 * there's work to do.
 *
 * One-click reconcile uses the same endpoint as the bulk page
 * (`/admin/waitlist/orphans`) — the mutation invalidates this
 * card's status query on success so the card disappears without
 * a reload. Errors surface via the global mutation toast
 * (meta.errorMessage on useReconcileOrphans).
 */
function OrphanStatusCard({ userId }: { userId: string }) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { data: status, isLoading } = useAdminUserOrphanStatus(userId);
  const reconcile = useReconcileOrphans();

  if (isLoading || !status?.is_orphan || !status.candidate) {
    return null;
  }

  const { candidate } = status;
  const expiresAt = new Date(candidate.invite_expires_at);
  const expiresDisplay = Number.isNaN(expiresAt.getTime())
    ? candidate.invite_expires_at
    : expiresAt.toLocaleDateString();

  const handleReconcile = () => {
    reconcile.mutate({ user_ids: [userId] });
  };

  return (
    <div className="mb-4 rounded-surface border border-amber-500/30 bg-amber-500/[0.06] p-5">
      <div className="flex items-start gap-3">
        <AlertTriangle
          className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
          aria-hidden
        />
        <div className="flex-1 min-w-0">
          <h2 className="text-[14px] font-medium text-fg-1">
            {t.admin.waitlist.orphans.userCardTitle}
          </h2>
          <p className="mt-1 text-[13px] leading-relaxed text-fg-2">
            {t.admin.waitlist.orphans.userCardDescription}
          </p>
          <p className="mt-2 text-[12px] text-fg-3 tabular-nums">
            {interpolate(t.admin.waitlist.orphans.userCardInviteExpires, {
              when: expiresDisplay,
            })}
          </p>
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={handleReconcile}
              disabled={reconcile.isPending}
              className="inline-flex items-center gap-1.5 rounded-md bg-amber-500/90 px-3 py-1.5 text-[13px] font-medium text-white transition-colors duration-[var(--dur-fast)] ease-[var(--ease-spring)] hover:bg-amber-500 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {reconcile.isPending
                ? t.admin.waitlist.orphans.reconcileRunning
                : t.admin.waitlist.orphans.reconcileOne}
            </button>
            <Link
              href="/admin/waitlist/orphans"
              className="inline-flex items-center gap-1 rounded-md px-2 py-1.5 text-[13px] font-medium text-amber-700 transition-colors hover:bg-amber-500/10 dark:text-amber-300"
            >
              {t.admin.waitlist.orphans.bannerCta}
              <ArrowRight className="h-3.5 w-3.5" aria-hidden />
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}

// Renders the effective_plan.source. Known shapes:
//   "hub:<id>"  → link to /admin/hubs/{id} using the hub's display name
//                 when the hub is in the user's membership list.
//                 Unknown hubs render plain text (no dead link — the
//                 admin may not have visibility into that hub, or it
//                 may have been deleted since the cache was written).
//   "personal"  → localized "personal" label.
// Other tokens (future resolver reasons like "override") fall through
// to raw display so we don't hide new signals behind a translation gap.
function EffectiveSource({
  source,
  hubs,
}: {
  source: string;
  hubs?: Array<{ hub: { id: string; name: string; hub_type: string } }>;
}) {
  const { t } = useLocale();
  const copy = t.admin.users.detail;
  if (source.startsWith("hub:")) {
    const hubId = source.slice(4);
    const hub = hubs?.find((h) => h.hub.id === hubId);
    if (hub) {
      return (
        <Link
          href={`/admin/hubs/${hubId}`}
          className="text-fg-2 hover:text-fg-1 underline underline-offset-2 decoration-border"
        >
          {copy.effectiveSourceHub.replace("{name}", hub.hub.name)}
        </Link>
      );
    }
    return (
      <span className="text-fg-2" title={hubId}>
        {copy.effectiveSourceUnknownHub.replace(
          "{id}",
          hubId.length > 8 ? `${hubId.slice(0, 8)}…` : hubId,
        )}
      </span>
    );
  }
  if (source === "personal") {
    return <span className="text-fg-2">{copy.effectiveSourcePersonal}</span>;
  }
  return <span className="text-fg-2">{source}</span>;
}
